# =========================
# IMPORTS
# =========================
import os
import time
import base64
import logging

from selenium import webdriver
from selenium.webdriver.chrome.service import Service
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC

from bs4 import BeautifulSoup


# =========================
# SETTINGS
# =========================
BASE_URL = "https://habr.com/ru/feed/"
PAGES_COUNT = 50

SAVE_FOLDER = "pdf_articles"

DELAY_BETWEEN_ARTICLES = 4   # пауза между статьями (сек)
DELAY_AFTER_LOAD = 2         # пауза после загрузки перед печатью (сек)
DELAY_BETWEEN_PAGES = 3      # пауза между страницами фида (сек)

CHROMEDRIVER_PATH = "/usr/bin/chromedriver"

HEADLESS = True

RETRY_COUNT = 3          # сколько раз повторять при ошибке
RETRY_DELAY = 5          # пауза между попытками (сек)
PAGE_TIMEOUT = 45        # максимальное ожидание загрузки страницы (сек)


# =========================
# LOGGING
# =========================
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[
        logging.FileHandler("parsing.log", encoding="utf-8"),
        logging.StreamHandler(),
    ]
)
log = logging.getLogger(__name__)


# =========================
# SETUP
# =========================
def ensure_folder():
    if not os.path.exists(SAVE_FOLDER):
        os.makedirs(SAVE_FOLDER)


def create_driver():
    options = Options()

    if HEADLESS:
        options.add_argument("--headless=new")

    options.add_argument("--no-sandbox")
    options.add_argument("--disable-dev-shm-usage")
    options.add_argument("--disable-gpu")
    options.add_argument("--window-size=1920,1080")

    # Антидетект — имитируем обычный браузер
    options.add_argument("--user-agent=Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
    options.add_argument("--disable-blink-features=AutomationControlled")
    options.add_experimental_option("excludeSwitches", ["enable-automation"])
    options.add_experimental_option("useAutomationExtension", False)

    # Не ждём полной загрузки всех ресурсов — только DOM
    options.page_load_strategy = "eager"

    service = Service(CHROMEDRIVER_PATH)
    driver = webdriver.Chrome(service=service, options=options)
    driver.set_page_load_timeout(PAGE_TIMEOUT)

    # Скрываем navigator.webdriver
    driver.execute_cdp_cmd("Page.addScriptToEvaluateOnNewDocument", {
        "source": "Object.defineProperty(navigator, 'webdriver', {get: () => undefined})"
    })

    return driver


def clean_filename(name):
    return "".join(c for c in name if c.isalnum() or c in " _-")[:100].strip()


def already_saved(index):
    """Проверяет, есть ли уже файл с данным индексом в папке."""
    for fname in os.listdir(SAVE_FOLDER):
        if fname.startswith(f"{index}_"):
            return True
    return False


# =========================
# LINKS
# =========================
def get_article_links(driver, page):
    url = BASE_URL if page == 1 else f"{BASE_URL}page{page}/"

    try:
        driver.get(url)
    except Exception:
        # page_load_strategy=eager может кидать timeout на медленные ресурсы — это нормально
        pass

    try:
        WebDriverWait(driver, PAGE_TIMEOUT).until(
            EC.presence_of_element_located((By.CLASS_NAME, "tm-title__link"))
        )
    except Exception:
        # Если ждать долго — попробуем взять что есть
        log.warning(f"Таймаут ожидания статей на странице {page}, беру что есть...")

    soup = BeautifulSoup(driver.page_source, "html.parser")

    links = []

    for a in soup.find_all("a", class_="tm-title__link"):
        href = a.get("href")

        if not href:
            continue

        if "/news/" in href:
            continue

        full_url = "https://habr.com" + href
        if full_url not in links:
            links.append(full_url)

    return links


# =========================
# SAVE PDF
# =========================
def save_page_as_pdf(driver, url, index):
    try:
        driver.get(url)
    except Exception:
        pass  # eager strategy — игнорируем timeout на медленные ресурсы

    try:
        WebDriverWait(driver, PAGE_TIMEOUT).until(
            EC.presence_of_element_located((By.TAG_NAME, "article"))
        )
    except Exception:
        # Статья могла загрузиться частично — попробуем сохранить
        pass

    time.sleep(DELAY_AFTER_LOAD)

    title = driver.title or "no_title"
    filename = clean_filename(title) or f"article_{index}"

    path = os.path.join(SAVE_FOLDER, f"{index}_{filename}.pdf")

    pdf = driver.execute_cdp_cmd("Page.printToPDF", {
        "printBackground": True
    })

    with open(path, "wb") as f:
        f.write(base64.b64decode(pdf["data"]))

    log.info(f"[OK] #{index} → {path}")


def save_with_retry(driver, url, index):
    """Пытается сохранить статью RETRY_COUNT раз с экспоненциальной паузой."""
    for attempt in range(1, RETRY_COUNT + 1):
        try:
            save_page_as_pdf(driver, url, index)
            return True
        except Exception as e:
            wait = RETRY_DELAY * (2 ** (attempt - 1))  # 5, 10, 20 сек
            log.warning(f"Попытка {attempt}/{RETRY_COUNT} не удалась для {url}: {e}")
            if attempt < RETRY_COUNT:
                log.info(f"  Жду {wait} сек перед следующей попыткой...")
                time.sleep(wait)

    log.error(f"[SKIP] #{index} {url} — все попытки исчерпаны")
    return False


# =========================
# MAIN
# =========================
def main():
    ensure_folder()

    driver = create_driver()

    # Собираем все ссылки сначала, чтобы знать общий объём
    all_links = []
    log.info("Собираю ссылки на статьи...")

    try:
        for page in range(1, PAGES_COUNT + 1):
            log.info(f"Страница {page}/{PAGES_COUNT}")
            try:
                links = get_article_links(driver, page)
                all_links.extend(links)
                log.info(f"  Найдено статей на странице: {len(links)} (всего: {len(all_links)})")
            except Exception as e:
                log.error(f"Не удалось получить ссылки со страницы {page}: {e}")

            if page < PAGES_COUNT:
                log.info(f"  Пауза {DELAY_BETWEEN_PAGES} сек перед следующей страницей...")
                time.sleep(DELAY_BETWEEN_PAGES)

        # Убираем дубли (на случай пересечения страниц)
        all_links = list(dict.fromkeys(all_links))
        log.info(f"\nВсего уникальных статей: {len(all_links)}")

        saved = 0
        failed = 0
        consecutive_errors = 0   # счётчик ошибок подряд

        for index, url in enumerate(all_links, start=1):
            if already_saved(index):
                log.info(f"[SKIP] #{index} уже скачана, пропускаю")
                saved += 1
                consecutive_errors = 0
                continue

            log.info(f"Скачиваю [{index}/{len(all_links)}]: {url}")
            success = save_with_retry(driver, url, index)

            if success:
                saved += 1
                consecutive_errors = 0
            else:
                failed += 1
                consecutive_errors += 1

            # Если 5 ошибок подряд — скорее всего бан, делаем длинную паузу
            if consecutive_errors >= 5:
                log.warning("5 ошибок подряд — возможен бан. Пауза 60 сек...")
                time.sleep(60)
                consecutive_errors = 0
            else:
                time.sleep(DELAY_BETWEEN_ARTICLES)

    finally:
        driver.quit()
        log.info(f"\n=== ГОТОВО ===")
        log.info(f"Скачано: {saved} | Ошибки: {failed}")
        log.info(f"Файлы в папке: {SAVE_FOLDER}/")
        log.info(f"Подробный лог: parsing.log")


if __name__ == "__main__":
    main()