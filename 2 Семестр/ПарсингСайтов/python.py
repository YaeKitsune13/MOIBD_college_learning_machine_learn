import json
import csv
import time
import random
from concurrent.futures import ThreadPoolExecutor
from playwright.sync_api import sync_playwright
from bs4 import BeautifulSoup

# Попробуем импортировать stealth аккуратно
try:
    from playwright_stealth import stealth_sync
    HAS_STEALTH = True
except ImportError:
    HAS_STEALTH = False

# --- НАСТРОЙКИ ---
BASE_URL = "https://www.imdb.com/chart/top/"
# В Arch/CachyOS путь обычно /usr/bin/chromium
CHROMIUM_PATH = "/usr/bin/chromium" 
MAX_WORKERS = 4
LIMIT = 250

def scrape_movie_details(browser_context, item_data):
    page = browser_context.new_page()
    if HAS_STEALTH:
        stealth_sync(page)
    
    url = f"https://www.imdb.com/title/{item_data['id']}/"
    
    try:
        time.sleep(random.uniform(0.8, 1.5))
        page.goto(url, wait_until="domcontentloaded", timeout=60000)
        page.wait_for_selector('h1', timeout=60000)
        
        html = page.content()
        s = BeautifulSoup(html, "html.parser")

        # --- 1. Описание ---
        desc_tag = s.find("span", {"data-testid": "plot-xs_to_m"}) or \
                   s.find("span", {"data-testid": "plot-xl"}) or \
                   s.select_one('[data-testid="plot"]')
        description = desc_tag.get_text(strip=True) if desc_tag else "No description"

        # --- 2. ЖАНРЫ (Улучшенный поиск) ---
        # Ищем все ссылки, которые ведут на поиск по жанрам
        genre_links = s.find_all("a", href=lambda x: x and "/search/title?genres=" in x)
        # Извлекаем текст и убираем дубликаты
        genres_list = []
        for g in genre_links:
            text = g.get_text(strip=True)
            if text and text not in genres_list:
                genres_list.append(text)
        
        genres = ", ".join(genres_list[:3]) # Берем первые 3

        # --- 3. СТРАНА ---
        country = "N/A"
        # Ищем блок, содержащий 'Origin' или 'Country of origin'
        country_box = s.find("li", {"data-testid": "title-details-origin"})
        if country_box:
            country_link = country_box.find("a")
            if country_link:
                country = country_link.get_text(strip=True)

        print(f"✅ [{item_data['rank']}] {item_data['title']} | Жанры: {genres}")
        page.close()
        
        return {
            "rank": item_data['rank'],
            "title": item_data['title'],
            "year": item_data['year'],
            "country": country,
            "genres": genres,
            "description": description
        }
    except Exception as e:
        print(f"❌ Ошибка на {item_data['title']}: {e}")
        page.close()
        return None

def worker(movie_chunk):
    with sync_playwright() as p:
        # Запускаем системный Chromium с флагами скрытия автоматизации
        browser = p.chromium.launch(
            executable_path=CHROMIUM_PATH,
            headless=False,
            args=[
                "--disable-blink-features=AutomationControlled", # Прячет флаг робота
                "--start-maximized"
            ]
        )
        # Создаем контекст с обычным User-Agent
        context = browser.new_context(
            user_agent="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
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

    with sync_playwright() as p:
        print(f"🔍 Запуск браузера...")
        browser = p.chromium.launch(
            executable_path=CHROMIUM_PATH, 
            headless=False,
            args=["--disable-blink-features=AutomationControlled"]
        )
        context = browser.new_context()
        page = context.new_page()
        if HAS_STEALTH: stealth_sync(page)
        
        try:
            page.goto(BASE_URL, wait_until="networkidle", timeout=120000)
            page.wait_for_selector('li.ipc-metadata-list-summary-item', timeout=20000)
            
            script_content = page.locator("script#__NEXT_DATA__").inner_text()
            data = json.loads(script_content)
            
            try:
                edges = data["props"]["pageProps"]["pageData"]["chartTitles"]["edges"]
            except KeyError:
                edges = data["props"]["pageProps"]["mainColumnData"]["chartTitles"]["edges"]

            for i, item in enumerate(edges[:LIMIT]):
                node = item["node"]
                all_movies_list.append({
                    "id": node["id"],
                    "rank": i + 1,
                    "title": node["titleText"]["text"],
                    "year": node["releaseYear"]["year"]
                })
            
            print(f"🎯 Список готов. Начинаем сбор деталей...")
        except Exception as e:
            print(f"🚨 Ошибка: {e}")
            return
        finally:
            browser.close()

    if not all_movies_list: return

    chunk_size = (len(all_movies_list) + MAX_WORKERS - 1) // MAX_WORKERS
    chunks = [all_movies_list[i:i + chunk_size] for i in range(0, len(all_movies_list), chunk_size)]

    final_results = []
    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
        futures = [executor.submit(worker, chunk) for chunk in chunks]
        for f in futures:
            res = f.result()
            if res: final_results.extend(res)

    final_results.sort(key=lambda x: x['rank'])
    with open("imdb_results.csv", "w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=["rank", "title", "year", "country", "genres", "description"])
        writer.writeheader()
        writer.writerows(final_results)

    print(f"\n✨ Завершено за {round((time.time()-start_time)/60, 2)} мин.")

if __name__ == "__main__":
    main()