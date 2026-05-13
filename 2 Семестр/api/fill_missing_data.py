import csv
import requests
import time
from deep_translator import GoogleTranslator

API_KEY = "M5NR8PT-DWX44KT-HBT1WVC-CE0GP91"
INPUT_FILE = "movies_updated.csv"
OUTPUT_FILE = "movies_filled.csv"

url = "https://api.kinopoisk.dev/v1.4/movie/search"
headers = {"X-API-KEY": API_KEY}

movies = []
with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        movies.append(row)

print(f"Всего фильмов: {len(movies)}")

# Считаем пропуски
missing_description = sum(1 for m in movies if not m.get("description", "").strip())
missing_country = sum(1 for m in movies if not m.get("country", "").strip())
missing_genres = sum(1 for m in movies if not m.get("genres", "").strip())

print(f"Пропущено описаний: {missing_description}")
print(f"Пропущено стран: {missing_country}")
print(f"Пропущено жанров: {missing_genres}")


def translate_to_russian(text, retries=3):
    """Переводит название фильма на русский"""
    if not text:
        return None
    
    for attempt in range(1, retries + 1):
        try:
            translator = GoogleTranslator(source='auto', target='ru')
            result = translator.translate(text)
            return result if result else None
        except Exception as e:
            if attempt < retries:
                wait = 2 * attempt
                print(f"    ⚠ Ошибка перевода, попытка {attempt}/{retries}, ждём {wait}с...")
                time.sleep(wait)
            else:
                print(f"    ✗ Не удалось перевести: {text}")
                return None
    return None


def fetch_movie_data(title, year, retries=5):
    """Ищет фильм в Кинопоиске по названию и году"""
    params = (
        ("query", title),
        ("year", year),
        ("limit", 10),
        ("type", "movie"),
    )
    
    for attempt in range(1, retries + 1):
        try:
            response = requests.get(url, headers=headers, params=params, timeout=10)

            if response.status_code == 200:
                docs = response.json().get("docs", [])
                
                # Ищем точное совпадение по году
                for doc in docs:
                    if str(doc.get("year")) == str(year):
                        return extract_movie_info(doc)
                
                # Если не нашли по году, берём первый результат
                if docs:
                    return extract_movie_info(docs[0])
                
                return None

            elif response.status_code == 403:
                wait = 30 * attempt
                print(f"    ⚠ 403 Лимит запросов, ждём {wait}с (попытка {attempt}/{retries})...")
                time.sleep(wait)

            elif response.status_code == 429:
                wait = 15 * attempt
                print(f"    ⚠ 429 Rate limit, ждём {wait}с...")
                time.sleep(wait)

            elif response.status_code in (502, 503, 504):
                wait = 5 * attempt
                print(f"    ⚠ Сервер недоступен ({response.status_code}), попытка {attempt}/{retries}...")
                time.sleep(wait)

            else:
                print(f"    ✗ Ошибка API: {response.status_code}")
                return None

        except requests.exceptions.Timeout:
            print(f"    ⚠ Таймаут, попытка {attempt}/{retries}...")
            time.sleep(5 * attempt)
        except requests.exceptions.ConnectionError:
            print(f"    ⚠ Ошибка соединения, попытка {attempt}/{retries}...")
            time.sleep(5 * attempt)

    print(f"    ✗ Не удалось получить данные после {retries} попыток")
    return None


def extract_movie_info(doc):
    """Извлекает нужную информацию из документа API"""
    return {
        "country": ", ".join([c["name"] for c in doc.get("countries", [])]),
        "genres": ", ".join([g["name"] for g in doc.get("genres", [])]),
        "description": doc.get("description", "").strip(),
    }


def save_progress():
    """Сохраняет текущий прогресс"""
    movies_sorted = sorted(movies, key=lambda x: float(x.get("rating_imdb") or 0), reverse=True)
    for i, m in enumerate(movies_sorted):
        m["rank"] = str(i + 1)
    
    with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
        fieldnames = ["rank", "title", "year", "country", "genres", "description", "rating_kp", "rating_imdb", "votes_imdb"]
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(movies_sorted)


# Обрабатываем фильмы с пропусками
processed = 0
updated = 0

for i, movie in enumerate(movies):
    title = movie.get("title", "").strip()
    year = movie.get("year", "").strip()
    
    has_missing = (
        not movie.get("description", "").strip() or
        not movie.get("country", "").strip() or
        not movie.get("genres", "").strip()
    )
    
    if not has_missing or not title or not year:
        continue
    
    print(f"\n[{i+1}/{len(movies)}] Обработка: {title} ({year})")
    
    # Шаг 1: Переводим названиев на русский
    russian_title = title
    if not movie.get("description", "").strip():
        print(f"  🔄 Перевожу название на русский...")
        translated = translate_to_russian(title)
        if translated and translated.lower() != title.lower():
            russian_title = translated
            print(f"  ✓ Перевод: {title} → {russian_title}")
        time.sleep(0.5)
    
    # Шаг 2: Ищем данные по переводному названию
    print(f"  🔍 Ищу в Кинопоиске по названию: {russian_title}")
    data = fetch_movie_data(russian_title, year)
    
    if not data:
        # Если не нашли по переводному названию, ищем по оригиналу
        print(f"  🔍 Ищу по оригинальному названию: {title}")
        data = fetch_movie_data(title, year)
    
    if data:
        # Заполняем пропуски
        if not movie.get("description", "").strip() and data.get("description"):
            movie["description"] = data["description"]
            print(f"  ✓ Добавлено описание: {data['description'][:60]}...")
        
        if not movie.get("country", "").strip() and data.get("country"):
            movie["country"] = data["country"]
            print(f"  ✓ Добавлена страна: {data['country']}")
        
        if not movie.get("genres", "").strip() and data.get("genres"):
            movie["genres"] = data["genres"]
            print(f"  ✓ Добавлены жанры: {data['genres']}")
        
        updated += 1
    else:
        print(f"  ✗ Фильм не найден")
    
    processed += 1
    
    # Сохраняем прогресс каждые 20 фильмов
    if processed % 20 == 0:
        save_progress()
        print(f"  💾 Прогресс сохранён ({processed} обработано, {updated} обновлено)")
    
    # Задержка между запросами
    time.sleep(1.5)

save_progress()
print(f"\n✅ Готово!")
print(f"  Обработано: {processed} фильмов")
print(f"  Обновлено: {updated} фильмов")
print(f"  Результат в файле: {OUTPUT_FILE}")
