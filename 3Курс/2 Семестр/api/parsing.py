import csv
import requests
import time

API_KEY = "M5NR8PT-DWX44KT-HBT1WVC-CE0GP91"
INPUT_FILE = "movies_ru.csv"
OUTPUT_FILE = "movies_updated.csv"
TARGET_COUNT = 500

url = "https://api.kinopoisk.dev/v1.4/movie"
headers = {"X-API-KEY": API_KEY}

movies = []
existing = set()
with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        key = (row["title"].strip().lower(), row["year"])
        existing.add(key)
        row.setdefault("rating_kp", "0")
        row.setdefault("rating_imdb", "0")
        row.setdefault("votes_imdb", "0")
        movies.append(row)

print(f"Уже есть: {len(movies)} фильмов")


def fetch_page(page, retries=5):
    params = (
        ("page", page),
        ("limit", 50),
        ("sortField", "votes.imdb"),
        ("sortType", "-1"),
        ("type", "movie"),
        ("notNullFields", "rating.imdb"),
        ("notNullFields", "description"),
    )
    for attempt in range(1, retries + 1):
        try:
            response = requests.get(url, headers=headers, params=params, timeout=15)
            if response.status_code == 200:
                return response.json()
            elif response.status_code == 429:
                wait = 10 * attempt
                print(f"  ⚠ Rate limit, ждём {wait}с...")
                time.sleep(wait)
            elif response.status_code in (502, 503, 504):
                wait = 5 * attempt
                print(f"  ⚠ Сервер недоступен ({response.status_code}), попытка {attempt}/{retries}, ждём {wait}с...")
                time.sleep(wait)
            else:
                print(f"  ✗ Ошибка API: {response.status_code}")
                return None
        except requests.exceptions.Timeout:
            wait = 5 * attempt
            print(f"  ⚠ Таймаут, попытка {attempt}/{retries}, ждём {wait}с...")
            time.sleep(wait)
        except requests.exceptions.ConnectionError:
            wait = 5 * attempt
            print(f"  ⚠ Ошибка соединения, попытка {attempt}/{retries}, ждём {wait}с...")
            time.sleep(wait)
    print(f"  ✗ Страница {page} недоступна после {retries} попыток, пропускаем")
    return None


def save_progress():
    sorted_movies = sorted(movies, key=lambda x: float(x.get("rating_imdb") or 0), reverse=True)
    for i, m in enumerate(sorted_movies):
        m["rank"] = str(i + 1)
    with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
        fieldnames = ["rank", "title", "year", "country", "genres", "description", "rating_kp", "rating_imdb", "votes_imdb"]
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(sorted_movies)


page = 1
consecutive_errors = 0

while len(movies) < TARGET_COUNT:
    data = fetch_page(page)

    if data is None:
        consecutive_errors += 1
        if consecutive_errors >= 3:
            print("✗ 3 страницы подряд недоступны, сохраняем и выходим")
            break
        page += 1
        continue

    consecutive_errors = 0
    docs = data.get("docs", [])
    total_pages = data.get("pages", 1)

    if not docs:
        print("Фильмы закончились")
        break

    added_on_page = 0
    for movie in docs:
        name = movie.get("name") or movie.get("alternativeName")
        year = movie.get("year")
        description = movie.get("description", "").strip()

        if not name or not year or not description:
            continue

        key = (name.strip().lower(), str(year))
        if key in existing:
            continue

        genres = ", ".join([g["name"] for g in movie.get("genres", [])])
        countries = ", ".join([c["name"] for c in movie.get("countries", [])])

        rating = movie.get("rating", {})
        rating_kp = round(rating.get("kp") or 0, 1)
        rating_imdb = round(rating.get("imdb") or 0, 1)

        votes = movie.get("votes", {})
        votes_imdb = votes.get("imdb") or 0

        new_movie = {
            "rank": str(len(movies) + 1),
            "title": name,
            "year": str(year),
            "country": countries,
            "genres": genres,
            "description": description,
            "rating_kp": str(rating_kp),
            "rating_imdb": str(rating_imdb),
            "votes_imdb": str(votes_imdb)
        }
        movies.append(new_movie)
        existing.add(key)
        added_on_page += 1
        print(f"[{len(movies)}/{TARGET_COUNT}] {name} ({year}) | KP: {rating_kp} | IMDb: {rating_imdb}")

        if len(movies) >= TARGET_COUNT:
            break

    print(f"  → Страница {page}/{total_pages}, добавлено: {added_on_page}")

    if page % 5 == 0:
        save_progress()
        print(f"  💾 Прогресс сохранён ({len(movies)} фильмов)")

    if page >= total_pages:
        print("Достигнут конец списка")
        break

    page += 1
    time.sleep(0.5)

save_progress()
print(f"\nГотово! Итого {len(movies)} фильмов.")
for m in movies[:5]:
    print(f"  {m.get('rank', '?')}. {m.get('title', '?')} ({m.get('year', '?')}) — IMDb: {m.get('rating_imdb', 'н/д')}")
    print(f"       {m.get('description', '')[:80]}...")