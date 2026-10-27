# =========================
# IMPORTS
# =========================
import os
import re
import glob
import csv
import logging

import fitz  # pip install PyMuPDF


# =========================
# НАСТРОЙКИ
# =========================
PDF_DIR    = os.path.expanduser("~/pdf_articles/")
OUTPUT_CSV = os.path.expanduser("~/Projects/MOiBD/habr_articles.csv")


# =========================
# LOGGING
# =========================
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[
        logging.FileHandler("habr_fast_parser.log", encoding="utf-8"),
        logging.StreamHandler(),
    ]
)
log = logging.getLogger(__name__)


# =========================
# PDF → ТЕКСТ
# =========================
def extract_text(pdf_path: str) -> tuple[list[str], str]:
    doc   = fitz.open(pdf_path)
    pages = [doc.load_page(i).get_text("text") for i in range(len(doc))]
    doc.close()
    return pages, "\n".join(pages)


# =========================
# ПАРСЕР
# =========================
def parse(pdf_path: str) -> dict:
    pages, full_text = extract_text(pdf_path)

    result = {
        "filename":            os.path.basename(pdf_path),
        "title":               "",
        "author":              "",
        "publish_time":        "",
        "company":             "",
        "company_description": "",
        "rating":              "",
        "subscribers":         "",
        "read_time":           "",
        "views":               "",
        "difficulty":          "",
        "hubs":                "",
        "tags":                "",
        "text":                "",
    }

    if not pages:
        return result

    lines = [l.strip() for l in pages[0].split("\n") if l.strip()]

    # ----- Рейтинг + компания -----
    for i, line in enumerate(lines):
        m = re.match(r'^([\d,\.]+)\s+Рейтинг$', line)
        if m:
            result["rating"] = m.group(1)
            if i >= 2:
                result["company"]             = lines[i - 2]
                result["company_description"] = lines[i - 1]
            elif i == 1:
                result["company"] = lines[0]
            break

    # ----- Подписчики -----
    for line in lines:
        m = re.match(r'^([\d\s]+)\s+Подписчик', line)
        if m:
            result["subscribers"] = m.group(1).strip()
            break

    # ----- Автор + время публикации -----
    time_re = re.compile(
        r'(назад|\d+\s*(мин|час|дн|сек)|\d+\s+(янв|фев|мар|апр|май|июн|июл|авг|сен|окт|ноя|дек))'
    )
    author_line_idx = None
    for i, line in enumerate(lines):
        if re.match(r'^[a-zA-Z0-9_]+$', line) and i + 1 < len(lines):
            if time_re.search(lines[i + 1]):
                result["author"]       = line
                result["publish_time"] = lines[i + 1]
                author_line_idx        = i
                break

    # ----- Заголовок (строки после времени публикации, до стоп-слова) -----
    stop_words   = {"Простой", "Средний", "Сложный", "Подписаться", "Комментировать"}
    stop_pattern = re.compile(r'^(\d+\s*мин|\d+\s*K|\d{3,}|Блог компании|Хабы:|Теги:)')

    if result["publish_time"]:
        pub_idx = None
        for i, line in enumerate(lines):
            if line == result["publish_time"]:
                pub_idx = i
                break
        if pub_idx is not None:
            title_parts = []
            for line in lines[pub_idx + 1:]:
                if line in stop_words or stop_pattern.match(line):
                    break
                if len(line) > 3:
                    title_parts.append(line)
                if len(title_parts) >= 3:
                    break
            result["title"] = " ".join(title_parts).strip()

    # Fallback заголовок из имени файла
    if not result["title"]:
        name = os.path.basename(pdf_path)
        name = re.sub(r'^\d+_', '', name)
        name = re.sub(r'\s*_?\s*Хабр\.pdf$', '', name, flags=re.IGNORECASE)
        name = name.replace(".pdf", "")
        result["title"] = name.strip()

    # ----- Сложность -----
    for line in lines:
        if line in ("Простой", "Средний", "Сложный"):
            result["difficulty"] = line
            break

    # ----- Время чтения -----
    for line in lines:
        m = re.match(r'^(\d+)\s+мин$', line)
        if m:
            result["read_time"] = m.group(1)
            break

    # ----- Просмотры -----
    # Формат: число рядом с иконкой глаза — ищем изолированное большое число
    for line in lines:
        m = re.match(r'^(\d[\d\s]{2,6})$', line)
        if m:
            num = m.group(1).replace(" ", "")
            if 100 <= int(num) <= 9_000_000:
                result["views"] = num
                break

    # ----- Теги (поиск по всему тексту) -----
    tags_m = re.search(r'Теги:\s*(.+?)(?:\n\n|\Z)', full_text, re.DOTALL)
    if tags_m:
        raw_tags = tags_m.group(1).replace("\n", " ").strip()
        result["tags"] = re.sub(r'\s+', ' ', raw_tags)

    # ----- Хабы (поиск по всему тексту) -----
    hubs_m = re.search(r'Хабы:\s*(.+?)(?:\n\n|\Z)', full_text, re.DOTALL)
    if hubs_m:
        raw_hubs = hubs_m.group(1).replace("\n", " ").strip()
        result["hubs"] = re.sub(r'\s+', ' ', raw_hubs)
    else:
        # Альтернатива: строка содержит "Блог компании" или несколько хабов через запятую
        for line in lines:
            if "Блог компании" in line or re.search(r'\w+,\s*\w+', line):
                cleaned = line.replace('\xa0', ' ').replace('*', '').strip()
                if len(cleaned) > 10:
                    result["hubs"] = cleaned
                    break

    # ----- Полный текст -----
    result["text"] = full_text.replace("\n", " ").strip()

    return result


# =========================
# CSV
# =========================
CSV_COLUMNS = [
    "filename", "title", "author", "publish_time",
    "company", "company_description", "rating", "subscribers",
    "difficulty", "read_time", "views",
    "hubs", "tags", "text",
]

def init_csv(path: str):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="", encoding="utf-8-sig") as f:
        csv.DictWriter(f, fieldnames=CSV_COLUMNS).writeheader()

def append_csv(path: str, row: dict):
    with open(path, "a", newline="", encoding="utf-8-sig") as f:
        csv.DictWriter(f, fieldnames=CSV_COLUMNS, extrasaction="ignore").writerow(row)


# =========================
# MAIN
# =========================
def main():
    all_pdf = sorted(glob.glob(os.path.join(PDF_DIR, "*.pdf")))
    log.info(f"Найдено PDF: {len(all_pdf)}")

    if not all_pdf:
        log.error(f"PDF не найдены в {PDF_DIR}")
        return

    init_csv(OUTPUT_CSV)

    success = 0
    failed  = 0

    for i, pdf_path in enumerate(all_pdf, 1):
        fname = os.path.basename(pdf_path)
        try:
            row = parse(pdf_path)
            append_csv(OUTPUT_CSV, row)
            log.info(f"[{i}/{len(all_pdf)}] ✓ {fname[:65]}")
            success += 1
        except Exception as e:
            log.error(f"[{i}/{len(all_pdf)}] ✗ {fname[:50]}: {e}")
            failed += 1

    log.info(f"\n=== ГОТОВО === Успешно: {success} | Ошибок: {failed}")
    log.info(f"CSV: {OUTPUT_CSV}")


if __name__ == "__main__":
    main()