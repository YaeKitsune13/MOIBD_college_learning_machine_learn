import csv
import requests
import time

API_KEY = "<set ur api>"
INPUT_FILE = "movies_ru.csv"
OUTPUT_FILE = "movies_updated.csv"

TARGET_COUNT = 500

url = "https://api.kinopoisk.dev/v1.4/movie"

headers = {
    "X-API-KEY": API_KEY
}

# Загружаем существующие фильмы
movies = []
existing = set()

with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        key = (row["title"].strip().lower(), row["year"])
        existing.add(key)
        movies.append(row)

print(f"Уже есть: {len(movies)} фильмов")

page = 1

while len(movies) < TARGET_COUNT:
    params = {
        "page": page,
        "limit": 50
    }

    response = requests.get(url, headers=headers, params=params)

    if response.status_code != 200:
        print("Ошибка API:", response.status_code)
        break

    data = response.json()
    docs = data.get("docs", [])

    if not docs:
        print("Фильмы закончились")
        break

    for movie in docs:
        name = movie.get("name") or movie.get("alternativeName")
        year = movie.get("year")

        if not name or not year:
            continue

        key = (name.strip().lower(), str(year))

        if key in existing:
            continue

        genres = ", ".join([g["name"] for g in movie.get("genres", [])])
        countries = ", ".join([c["name"] for c in movie.get("countries", [])])
        description = movie.get("description") or ""

        new_movie = {
            "rank": str(len(movies) + 1),
            "title": name,
            "year": str(year),
            "country": countries,
            "genres": genres,
            "description": description
        }

        movies.append(new_movie)
        existing.add(key)

        print(f"Добавлен: {name} ({year})")

        if len(movies) >= TARGET_COUNT:
            break

    page += 1
    time.sleep(0.2)

# Сохраняем
with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
    fieldnames = ["rank", "title", "year", "country", "genres", "description"]
    writer = csv.DictWriter(f, fieldnames=fieldnames)

    writer.writeheader()
    writer.writerows(movies)

print(f"Готово! Теперь {len(movies)} фильмов.")