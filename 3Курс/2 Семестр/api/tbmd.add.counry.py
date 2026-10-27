import csv
import os
import time

import requests

API_KEY = "2c18c74fdd5dd14c83baf3056bf8e07f"

INPUT_FILE = "movies_tmdb.csv"
OUTPUT_FILE = "movies_tmdb.csv"
BASE_URL = "https://api.themoviedb.org/3"

# Жанры на русском
GENRES_RU = {
    28: "боевик",
    12: "приключения",
    16: "мультфильм",
    35: "комедия",
    80: "криминал",
    99: "документальный",
    18: "драма",
    10751: "семейный",
    14: "фэнтези",
    36: "история",
    27: "ужасы",
    10402: "музыка",
    9648: "детектив",
    10749: "мелодрама",
    878: "фантастика",
    10770: "телефильм",
    53: "триллер",
    10752: "военный",
    37: "вестерн",
}

# Страны на русском
COUNTRIES_RU = {
    "United States of America": "США",
    "United Kingdom": "Великобритания",
    "France": "Франция",
    "Germany": "Германия",
    "Japan": "Япония",
    "South Korea": "Южная Корея",
    "China": "Китай",
    "Italy": "Италия",
    "Spain": "Испания",
    "Canada": "Канада",
    "Australia": "Австралия",
    "India": "Индия",
    "Russia": "Россия",
    "Soviet Union": "СССР",
    "Sweden": "Швеция",
    "Denmark": "Дания",
    "Norway": "Норвегия",
    "Finland": "Финляндия",
    "Poland": "Польша",
    "Czech Republic": "Чехия",
    "Austria": "Австрия",
    "Switzerland": "Швейцария",
    "Belgium": "Бельгия",
    "Netherlands": "Нидерланды",
    "Portugal": "Португалия",
    "Mexico": "Мексика",
    "Brazil": "Бразилия",
    "Argentina": "Аргентина",
    "Ireland": "Ирландия",
    "New Zealand": "Новая Зеландия",
    "Hong Kong": "Гонконг",
    "Taiwan": "Тайвань",
    "Thailand": "Таиланд",
    "Iran": "Иран",
    "Turkey": "Турция",
    "Greece": "Греция",
    "Hungary": "Венгрия",
    "Romania": "Румыния",
    "Israel": "Израиль",
}


def get(url, params={}, retries=5):
    params["api_key"] = API_KEY
    for attempt in range(1, retries + 1):
        try:
            r = requests.get(url, params=params, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                wait = 10 * attempt
                print(f"  ⚠ Rate limit, ждём {wait}с...")
                time.sleep(wait)
            else:
                print(f"  ✗ Ошибка: {r.status_code}")
                return None
        except Exception as e:
            print(f"  ⚠ {e}, попытка {attempt}")
            time.sleep(5 * attempt)
    return None


def search_movie_id(title, year):
    """Ищем ID фильма по названию и году"""
    data = get(
        f"{BASE_URL}/search/movie",
        {
            "query": title,
            "year": year,
            "language": "ru-RU",
        },
    )
    if not data:
        return None
    results = data.get("results", [])
    if results:
        return results[0]["id"]
    return None


def fetch_details(movie_id):
    """Получаем страну и жанры по ID"""
    data = get(f"{BASE_URL}/movie/{movie_id}", {"language": "ru-RU"})
    if not data:
        return "", ""

    # Страна
    countries = data.get("production_countries", [])
    country_names = []
    for c in countries:
        en_name = c.get("name", "")
        country_names.append(COUNTRIES_RU.get(en_name, en_name))
    country = ", ".join(country_names)

    # Жанры из деталей (с id → русское название)
    genres = data.get("genres", [])
    genre_names = []
    for g in genres:
        gid = g.get("id")
        genre_names.append(GENRES_RU.get(gid, g.get("name", "")))
    genres_str = ", ".join(genre_names)

    return country, genres_str


# Загружаем CSV
movies = []
with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        movies.append(row)

print(f"Загружено: {len(movies)} фильмов")

# Считаем сколько нужно дополнить
need_update = [m for m in movies if not m.get("country") or not m.get("genres")]
print(f"Нужно дополнить: {len(need_update)} фильмов")

updated = 0
for i, movie in enumerate(movies):
    if movie.get("country") and movie.get("genres"):
        continue  # уже есть — пропускаем

    title = movie.get("title", "")
    year = movie.get("year", "")

    # Ищем ID фильма
    movie_id = search_movie_id(title, year)
    if not movie_id:
        print(f"  ✗ Не найден: {title} ({year})")
        continue

    country, genres = fetch_details(movie_id)

    if country:
        movie["country"] = country
    if genres:
        movie["genres"] = genres

    updated += 1
    print(f"[{updated}/{len(need_update)}] {title} ({year}) | {country} | {genres}")

    # Сохраняем каждые 50 фильмов
    if updated % 50 == 0:
        with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
            fieldnames = [
                "rank",
                "title",
                "year",
                "country",
                "genres",
                "description",
                "rating_kp",
                "rating_imdb",
                "votes_imdb",
            ]
            writer = csv.DictWriter(f, fieldnames=fieldnames)
            writer.writeheader()
            writer.writerows(movies)
        print(f"  💾 Сохранено ({updated} обновлено)")

    time.sleep(0.4)  # два запроса на фильм — чуть больше пауза

# Финальное сохранение
with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
    fieldnames = [
        "rank",
        "title",
        "year",
        "country",
        "genres",
        "description",
        "rating_kp",
        "rating_imdb",
        "votes_imdb",
    ]
    writer = csv.DictWriter(f, fieldnames=fieldnames)
    writer.writeheader()
    writer.writerows(movies)

print(f"\n✅ Готово! Обновлено {updated} фильмов.")
