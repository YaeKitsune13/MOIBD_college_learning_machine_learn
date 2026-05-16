import csv
import time

import requests

API_KEY = "2c18c74fdd5dd14c83baf3056bf8e07f"
INPUT_FILE = "movies_tmdb.csv"
OUTPUT_FILE = "movies_tmdb.csv"
TARGET_COUNT = 25000

BASE_URL = "https://api.themoviedb.org/3"

GENRES = {
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

# Загружаем уже собранные фильмы
movies = []
existing = set()
with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        key = (row["title"].strip().lower(), row["year"])
        existing.add(key)
        movies.append(row)

print(f"Уже есть: {len(movies)} фильмов")


def get(url, params={}, retries=5):
    params["api_key"] = API_KEY
    for attempt in range(1, retries + 1):
        try:
            r = requests.get(url, params=params, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                time.sleep(10 * attempt)
            else:
                print(f"  ✗ Ошибка: {r.status_code}")
                return None
        except Exception as e:
            print(f"  ⚠ {e}, попытка {attempt}")
            time.sleep(5 * attempt)
    return None


def save_progress():
    sorted_movies = sorted(
        movies, key=lambda x: float(x.get("votes_imdb") or 0), reverse=True
    )
    for i, m in enumerate(sorted_movies):
        m["rank"] = str(i + 1)
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
        writer.writerows(sorted_movies)


# Запросы по диапазонам годов — так обходим лимит 500 страниц
year_ranges = [
    (2020, 2026),
    (2015, 2019),
    (2010, 2014),
    (2005, 2009),
    (2000, 2004),
    (1990, 1999),
    (1900, 1989),
]

for year_from, year_to in year_ranges:
    if len(movies) >= TARGET_COUNT:
        break

    print(f"\n📅 Период {year_from}–{year_to}")
    page = 1

    while len(movies) < TARGET_COUNT:
        data = get(
            f"{BASE_URL}/discover/movie",
            {
                "language": "ru-RU",
                "sort_by": "vote_count.desc",
                "vote_count.gte": 100,  # снижаем порог
                "include_adult": False,
                "primary_release_date.gte": f"{year_from}-01-01",
                "primary_release_date.lte": f"{year_to}-12-31",
                "page": page,
            },
        )

        if not data:
            break

        docs = data.get("results", [])
        total_pages = min(data.get("total_pages", 1), 500)

        if not docs:
            break

        added = 0
        for movie in docs:
            title = movie.get("title") or movie.get("original_title")
            year = str(movie.get("release_date", ""))[:4]
            description = (movie.get("overview") or "").strip()

            if not title or not year or not description:
                continue

            key = (title.strip().lower(), year)
            if key in existing:
                continue

            genres = ", ".join(
                [GENRES.get(g, str(g)) for g in movie.get("genre_ids", [])]
            )
            rating_imdb = round(movie.get("vote_average") or 0, 1)
            votes_imdb = movie.get("vote_count") or 0

            new_movie = {
                "rank": str(len(movies) + 1),
                "title": title,
                "year": year,
                "country": "",
                "genres": genres,
                "description": description,
                "rating_kp": "0",
                "rating_imdb": str(rating_imdb),
                "votes_imdb": str(votes_imdb),
            }
            movies.append(new_movie)
            existing.add(key)
            added += 1
            print(
                f"[{len(movies)}/{TARGET_COUNT}] {title} ({year}) | IMDb: {rating_imdb}"
            )

            if len(movies) >= TARGET_COUNT:
                break

        print(f"  → Стр. {page}/{total_pages}, добавлено: {added}")

        if page % 10 == 0:
            save_progress()
            print(f"  💾 Сохранено {len(movies)} фильмов")

        if page >= total_pages:
            break

        page += 1
        time.sleep(0.25)

save_progress()
print(f"\n✅ Готово! Итого {len(movies)} фильмов.")
