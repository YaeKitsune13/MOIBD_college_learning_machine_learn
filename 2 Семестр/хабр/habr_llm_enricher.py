# =========================
# IMPORTS
# =========================
import os
import re
import json
import csv
import time
import logging
import requests

import fitz  # PyMuPDF


# =========================
# НАСТРОЙКИ
# =========================
CSV_PATH   = os.path.expanduser("~/Projects/MOiBD/habr_articles.csv")
PDF_DIR    = os.path.expanduser("~/pdf_articles/")
LLAMA_URL  = "http://127.0.0.1:8080/v1/chat/completions"

MAX_TOKENS = 600
TEXT_LIMIT = 3000   # меньше чем раньше — LLM делает меньше работы

CHECKPOINT_FILE = "habr_enricher_checkpoint.json"

# Какие поля LLM должна заполнить (если они пустые)
LLM_FIELDS = {"title", "summary", "topic", "technologies", "difficulty"}


# =========================
# LOGGING
# =========================
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[
        logging.FileHandler("habr_enricher.log", encoding="utf-8"),
        logging.StreamHandler(),
    ]
)
log = logging.getLogger(__name__)


# =========================
# CHECKPOINT
# =========================
def load_checkpoint() -> set:
    if os.path.exists(CHECKPOINT_FILE):
        with open(CHECKPOINT_FILE, "r", encoding="utf-8") as f:
            return set(json.load(f).get("done", []))
    return set()

def save_checkpoint(done: set):
    with open(CHECKPOINT_FILE, "w", encoding="utf-8") as f:
        json.dump({"done": list(done)}, f, ensure_ascii=False)


# =========================
# CSV
# =========================
def read_csv(path: str) -> list[dict]:
    with open(path, newline="", encoding="utf-8-sig") as f:
        return list(csv.DictReader(f))

def write_csv(path: str, rows: list[dict], fieldnames: list[str]):
    with open(path, "w", newline="", encoding="utf-8-sig") as f:
        w = csv.DictWriter(f, fieldnames=fieldnames, extrasaction="ignore")
        w.writeheader()
        w.writerows(rows)


# =========================
# PDF → ТЕКСТ
# =========================
def get_pdf_text(filename: str) -> str:
    pdf_path = os.path.join(PDF_DIR, filename)
    if not os.path.exists(pdf_path):
        return ""
    doc   = fitz.open(pdf_path)
    pages = [doc.load_page(i).get_text("text") for i in range(len(doc))]
    doc.close()
    return "\n".join(pages)


# =========================
# LLM
# =========================
SYSTEM_PROMPT = """Ты — ассистент для извлечения данных из статей Habr.
Отвечай ТОЛЬКО валидным JSON, без пояснений и markdown.
Пустую строку "" или [] если поле не найдено."""

def build_prompt(text: str, missing_fields: set) -> str:
    fields_desc = {
        "title":        '"title": "точный заголовок статьи"',
        "summary":      '"summary": "краткое резюме 2-3 предложения"',
        "topic":        '"topic": "основная тема: ML, DevOps, Веб, Безопасность, Железо, Карьера и т.п."',
        "technologies": '"technologies": ["технологии", "языки", "инструменты упомянутые в статье"]',
        "difficulty":   '"difficulty": "Простой или Средний или Сложный"',
    }
    needed = {k: v for k, v in fields_desc.items() if k in missing_fields}
    fields_str = ",\n  ".join(needed.values())

    return f"""Извлеки из текста статьи только эти поля и верни JSON:

{{
  {fields_str}
}}

ТЕКСТ СТАТЬИ:
{text[:TEXT_LIMIT]}
"""

def repair_json(raw: str) -> str:
    lines = raw.rstrip().split("\n")
    while lines:
        last = lines[-1].rstrip().rstrip(",")
        if re.search(r'["\]\}]$', last):
            break
        lines.pop()
    if not lines:
        return raw
    body   = "\n".join(lines)
    body   = re.sub(r",\s*$", "", body.rstrip())
    suffix = "]" * max(body.count("[") - body.count("]"), 0)
    suffix += "}" * max(body.count("{") - body.count("}"), 0)
    return body + "\n" + suffix

def llm_fill(text: str, missing_fields: set) -> dict:
    empty  = {f: "" for f in missing_fields}
    prompt = build_prompt(text, missing_fields)
    raw    = ""

    try:
        resp = requests.post(
            LLAMA_URL,
            json={
                "messages": [
                    {"role": "system", "content": SYSTEM_PROMPT},
                    {"role": "user",   "content": prompt},
                ],
                "max_tokens":  MAX_TOKENS,
                "temperature": 0.1,
                "stop":        ["```"],
            },
            timeout=120,
        )
        resp.raise_for_status()
        raw = resp.json()["choices"][0]["message"]["content"].strip()
        raw = re.sub(r'^```(?:json)?\s*', '', raw)
        raw = re.sub(r'\s*```$', '', raw)

        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = json.loads(repair_json(raw))

        # technologies всегда список → строка
        if "technologies" in parsed:
            val = parsed["technologies"]
            if isinstance(val, list):
                parsed["technologies"] = "; ".join(str(v) for v in val if v)

        return {**empty, **parsed}

    except requests.exceptions.ConnectionError:
        log.error("llama-server недоступен!")
        raise
    except json.JSONDecodeError as e:
        log.warning(f"JSON ошибка: {e} | raw: {raw[:200]}")
        return empty
    except Exception as e:
        log.warning(f"LLM ошибка: {e}")
        return empty


# =========================
# ПРОВЕРКА СЕРВЕРА
# =========================
def check_server() -> bool:
    try:
        return requests.get("http://127.0.0.1:8080/health", timeout=5).status_code == 200
    except Exception:
        return False


# =========================
# MAIN
# =========================
def main():
    if not check_server():
        log.error("llama-server не отвечает! Запусти сервер и попробуй снова.")
        return

    log.info("llama-server доступен ✓")

    # Читаем CSV
    rows      = read_csv(CSV_PATH)
    fieldnames = list(rows[0].keys()) if rows else []

    # Добавляем новые колонки если их нет
    for col in ("summary", "topic", "technologies"):
        if col not in fieldnames:
            fieldnames.append(col)
            for row in rows:
                row[col] = ""

    log.info(f"Статей в CSV: {len(rows)}")

    done     = load_checkpoint()
    total    = len(rows)
    enriched = 0
    skipped  = 0
    failed   = 0

    for i, row in enumerate(rows, 1):
        fname = row.get("filename", "")

        if fname in done:
            skipped += 1
            continue

        # Определяем какие LLM-поля пустые
        missing = {
            f for f in LLM_FIELDS
            if f in fieldnames and not str(row.get(f, "")).strip()
        }

        if not missing:
            log.info(f"[{i}/{total}] SKIP (всё заполнено) {fname[:55]}")
            done.add(fname)
            skipped += 1
            continue

        log.info(f"[{i}/{total}] {fname[:60]} → заполняю: {', '.join(sorted(missing))}")
        t0 = time.time()

        try:
            text   = get_pdf_text(fname)
            if not text.strip():
                log.warning(f"  PDF не найден или пустой: {fname}")
                failed += 1
                continue

            filled = llm_fill(text, missing)

            # Обновляем только пустые поля
            for field, value in filled.items():
                if field in missing and value:
                    row[field] = value

            done.add(fname)
            save_checkpoint(done)

            # Сохраняем CSV сразу после каждой статьи — Ctrl+C не потеряет прогресс
            write_csv(CSV_PATH, rows, fieldnames)
            enriched += 1

            elapsed = time.time() - t0
            log.info(
                f"  ✓ {elapsed:.1f}s | "
                f"тема: {row.get('topic', '?'):12s} | "
                f"{row.get('title', '')[:45]}"
            )

        except requests.exceptions.ConnectionError:
            log.error("Сервер упал! Перезапусти llama-server и скрипт.")
            write_csv(CSV_PATH, rows, fieldnames)  # сохраняем что успели
            break
        except Exception as e:
            log.error(f"  ✗ {fname[:50]}: {e}")
            failed += 1

    write_csv(CSV_PATH, rows, fieldnames)
    log.info(f"\n=== ГОТОВО ===")
    log.info(f"Обогащено: {enriched} | Пропущено: {skipped} | Ошибок: {failed}")
    log.info(f"CSV обновлён: {CSV_PATH}")


if __name__ == "__main__":
    main()