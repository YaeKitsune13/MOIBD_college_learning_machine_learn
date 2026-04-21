# =========================
# IMPORTS
# =========================
import os
import time
import base64

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
PAGES_COUNT = 10

SAVE_FOLDER = "pdf_articles"

DELAY_BETWEEN_ARTICLES = 2
DELAY_AFTER_LOAD = 1

CHROMEDRIVER_PATH = "/usr/bin/chromedriver"

HEADLESS = True


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

    service = Service(CHROMEDRIVER_PATH)

    return webdriver.Chrome(service=service, options=options)


def clean_filename(name):
    return "".join(c for c in name if c.isalnum() or c in " _-")[:100]


# =========================
# LINKS
# =========================
def get_article_links(driver, page):
    url = BASE_URL if page == 1 else f"{BASE_URL}page{page}/"

    driver.get(url)

    WebDriverWait(driver, 10).until(
        EC.presence_of_element_located((By.TAG_NAME, "body"))
    )

    soup = BeautifulSoup(driver.page_source, "html.parser")

    links = []

    for a in soup.find_all("a", class_="tm-title__link"):
        href = a.get("href")

        if not href:
            continue

        if "/news/" in href:
            continue

        links.append("https://habr.com" + href)

    return links


# =========================
# SAVE PDF
# =========================
def save_page_as_pdf(driver, url, index):
    driver.get(url)

    WebDriverWait(driver, 10).until(
        EC.presence_of_element_located((By.TAG_NAME, "body"))
    )

    time.sleep(DELAY_AFTER_LOAD)

    title = driver.title or "no_title"
    filename = clean_filename(title)

    path = os.path.join(SAVE_FOLDER, f"{index}_{filename}.pdf")

    pdf = driver.execute_cdp_cmd("Page.printToPDF", {
        "printBackground": True
    })

    with open(path, "wb") as f:
        f.write(base64.b64decode(pdf["data"]))

    print(f"[OK] {path}")


# =========================
# MAIN
# =========================
def main():
    ensure_folder()

    driver = create_driver()
    index = 1

    try:
        for page in range(1, PAGES_COUNT + 1):
            print(f"\n--- PAGE {page} ---")

            links = get_article_links(driver, page)

            for link in links:
                print("Сохраняю:", link)

                try:
                    save_page_as_pdf(driver, link, index)
                    index += 1

                    time.sleep(DELAY_BETWEEN_ARTICLES)

                except Exception as e:
                    print("Ошибка:", e)

    finally:
        driver.quit()


if __name__ == "__main__":
    main()