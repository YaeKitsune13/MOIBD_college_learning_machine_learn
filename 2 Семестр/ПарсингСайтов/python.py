import json
import csv
import time
import random
from concurrent.futures import ThreadPoolExecutor
from playwright.sync_api import sync_playwright
from bs4 import BeautifulSoup

try:
    from playwright_stealth import stealth_sync
    HAS_STEALTH = True
except ImportError:
    HAS_STEALTH = False

# --- НАСТРОЙКИ ---
BASE_URL = "https://www.imdb.com/chart/top/"
CHROMIUM_PATH = "/usr/bin/chromium"
MAX_WORKERS = 4
LIMIT = 250


def find_genres_recursive(obj):
    """Рекурсивно ищет поле genres в JSON-структуре Next.js"""
    if isinstance(obj, dict):
        if "genres" in obj and isinstance(obj["genres"], list):
            result = [g.get("text", "") for g in obj["genres"] if g.get("text")]
            if result:
                return result
        for v in obj.values():
            result = find_genres_recursive(v)
            if result:
                return result
    elif isinstance(obj, list):
        for item in obj:
            result = find_genres_recursive(item)
            if result:
                return result
    return []


def extract_genres(soup):
    """Многоуровневый поиск жанров — пробует 4 способа по очереди"""
    genres_list = []

    # Способ 1: JSON-LD (application/ld+json) — самый стабильный
    json_ld = soup.find("script", {"type": "application/ld+json"})
    if json_ld:
        try:
            ld_data = json.loads(json_ld.string)
            if "genre" in ld_data:
                raw = ld_data["genre"]
                genres_list = [raw] if isinstance(raw, str) else list(raw)
        except Exception:
            pass

    # Способ 2: __NEXT_DATA__ — рекурсивный поиск поля genres
    if not genres_list:
        next_data_tag = soup.find("script", {"id": "__NEXT_DATA__"})
        if next_data_tag:
            try:
                nd = json.loads(next_data_tag.string)
                genres_list = find_genres_recursive(nd)
            except Exception:
                pass

    # Способ 3: ссылки /search/title?genres= (старая вёрстка)
    if not genres_list:
        seen = set()
        for a in soup.find_all("a", href=lambda x: x and "/search/title?genres=" in x):
            t = a.get_text(strip=True)
            if t and t not in seen:
                seen.add(t)
                genres_list.append(t)

    # Способ 4: data-testid chip / ipc-chip (новая вёрстка)
    if not genres_list:
        for chip in soup.select('[data-testid="genres"] a, [class*="ipc-chip"] span'):
            t = chip.get_text(strip=True)
            if t and t not in genres_list:
                genres_list.append(t)

    return genres_list[:3]


def scrape_movie_details(browser_context, item_data):
    page = browser_context.new_page()
    if HAS_STEALTH:
        stealth_sync(page)

    url = f"https://www.imdb.com/title/{item_data['id']}/"

    try:
        time.sleep(random.uniform(0.8, 1.5))
        page.goto(url, wait_until="domcontentloaded", timeout=60000)
        page.wait_for_selector("h1", timeout=60000)

        html = page.content()
        s = BeautifulSoup(html, "html.parser")

        # --- 1. Описание ---
        desc_tag = (
            s.find("span", {"data-testid": "plot-xs_to_m"})
            or s.find("span", {"data-testid": "plot-xl"})
            or s.select_one('[data-testid="plot"]')
        )
        description = desc_tag.get_text(strip=True) if desc_tag else "No description"

        # --- 2. Жанры (многоуровневый поиск) ---
        genres_list = extract_genres(s)
        genres = ", ".join(genres_list) if genres_list else "N/A"

        # --- 3. Страна ---
        country = "N/A"
        country_box = s.find("li", {"data-testid": "title-details-origin"})
        if country_box:
            country_link = country_box.find("a")
            if country_link:
                country = country_link.get_text(strip=True)

        print(f"✅ [{item_data['rank']}] {item_data['title']} | Жанры: {genres} | Страна: {country}")
        page.close()

        return {
            "rank": item_data["rank"],
            "title": item_data["title"],
            "year": item_data["year"],
            "country": country,
            "genres": genres,
            "description": description,
        }

    except Exception as e:
        print(f"❌ Ошибка на {item_data['title']}: {e}")
        page.close()
        return None


def worker(movie_chunk):
    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROMIUM_PATH,
            headless=False,
            args=[
                "--disable-blink-features=AutomationControlled",
                "--start-maximized",
            ],
        )
        context = browser.new_context(
            user_agent=(
                "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
                "(KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
            )
        )

        results = []
        for item in movie_chunk:
            res = scrape_movie_details(context, item)
            if res:
                results.append(res)

        browser.close()
    return results


def main():
    start_time = time.time()
    all_movies_list = []

    # --- Шаг 1: забираем список топ-250 ---
    with sync_playwright() as p:
        print("🔍 Запуск браузера для получения списка...")
        browser = p.chromium.launch(
            executable_path=CHROMIUM_PATH,
            headless=False,
            args=["--disable-blink-features=AutomationControlled"],
        )
        context = browser.new_context()
        page = context.new_page()
        if HAS_STEALTH:
            stealth_sync(page)

        try:
            page.goto(BASE_URL, wait_until="networkidle", timeout=120000)
            page.wait_for_selector("li.ipc-metadata-list-summary-item", timeout=20000)

            script_content = page.locator("script#__NEXT_DATA__").inner_text()
            data = json.loads(script_content)

            try:
                edges = data["props"]["pageProps"]["pageData"]["chartTitles"]["edges"]
            except KeyError:
                edges = data["props"]["pageProps"]["mainColumnData"]["chartTitles"]["edges"]

            for i, item in enumerate(edges[:LIMIT]):
                node = item["node"]
                all_movies_list.append(
                    {
                        "id": node["id"],
                        "rank": i + 1,
                        "title": node["titleText"]["text"],
                        "year": node["releaseYear"]["year"],
                    }
                )

            print(f"🎯 Получено {len(all_movies_list)} фильмов. Начинаем сбор деталей...")

        except Exception as e:
            print(f"🚨 Ошибка при получении списка: {e}")
            return
        finally:
            browser.close()

    if not all_movies_list:
        return

    # --- Шаг 2: параллельно собираем детали ---
    chunk_size = (len(all_movies_list) + MAX_WORKERS - 1) // MAX_WORKERS
    chunks = [
        all_movies_list[i : i + chunk_size]
        for i in range(0, len(all_movies_list), chunk_size)
    ]

    final_results = []
    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
        futures = [executor.submit(worker, chunk) for chunk in chunks]
        for f in futures:
            res = f.result()
            if res:
                final_results.extend(res)

    # --- Шаг 3: сохраняем CSV ---
    final_results.sort(key=lambda x: x["rank"])

    with open("imdb_results.csv", "w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(
            f,
            fieldnames=["rank", "title", "year", "country", "genres", "description"],
        )
        writer.writeheader()
        writer.writerows(final_results)

    elapsed = round((time.time() - start_time) / 60, 2)
    print(f"\n✨ Готово! {len(final_results)} фильмов сохранено в imdb_results.csv за {elapsed} мин.")


if __name__ == "__main__":
    main()
