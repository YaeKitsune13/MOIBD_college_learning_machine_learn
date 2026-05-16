import csv
import time

import requests

# Получить бесплатно на https://www.themoviedb.org/settings/api
API_KEY = "2c18c74fdd5dd14c83baf3056bf8e07f"
OUTPUT_FILE = "movies_tmdb.csv"
TARGET_COUNT = 10000

BASE_URL = "https://api.themoviedb.org/3"
headers = {"Authorization": f"Bearer {API_KEY}"}  # или через params api_key=

movies = []
existing = set()


def fetch_page(page, retries=5):
    params = {
        "language": "ru-RU",
        "sort_by": "vote_count.desc",
        "vote_count.gte": 1000,  # только фильмы с достаточным числом голосов
        "include_adult": False,
        "page": page,
    }
    for attempt in range(1, retries + 1):
        try:
            r = requests.get(
                f"{BASE_URL}/discover/movie",
                params={**params, "api_key": API_KEY},
                timeout=15,
            )
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
            wait = 5 * attempt
            print(f"  ⚠ {e}, попытка {attempt}/{retries}, ждём {wait}с...")
            time.sleep(wait)
    return None


def save_progress():
    sorted_movies = sorted(
        movies, key=lambda x: float(x.get("rating_imdb") or 0), reverse=True
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


# Жанры TMDB (id -> название на русском)
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

page = 1
consecutive_errors = 0

while len(movies) < TARGET_COUNT:
    data = fetch_page(page)

    if data is None:
        consecutive_errors += 1
        if consecutive_errors >= 3:
            print("✗ 3 ошибки подряд, сохраняем и выходим")
            break
        page += 1
        continue

    consecutive_errors = 0
    docs = data.get("results", [])
    total_pages = data.get("total_pages", 1)

    if not docs:
        print("Фильмы закончились")
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

        genres = ", ".join([GENRES.get(g, str(g)) for g in movie.get("genre_ids", [])])
        rating_imdb = round(movie.get("vote_average") or 0, 1)
        votes_imdb = movie.get("vote_count") or 0

        new_movie = {
            "rank": str(len(movies) + 1),
            "title": title,
            "year": year,
            "country": "",  # TMDB отдаёт страну отдельным запросом
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
            f"[{len(movies)}/{TARGET_COUNT}] {title} ({year}) | IMDb: {rating_imdb} | 👍 {votes_imdb}"
        )

        if len(movies) >= TARGET_COUNT:
            break

    print(f"  → Страница {page}/{total_pages}, добавлено: {added}")

    if page % 10 == 0:
        save_progress()
        print(f"  💾 Сохранено {len(movies)} фильмов")

    if page >= total_pages or page >= 500:  # TMDB отдаёт макс 500 страниц
        print("Достигнут конец")
        break

    page += 1
    time.sleep(0.3)

save_progress()
print(f"\n✅ Готово! Итого {len(movies)} фильмов.")
