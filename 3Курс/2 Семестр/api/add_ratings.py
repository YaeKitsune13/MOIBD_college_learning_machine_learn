import csv
import requests
import time

API_KEY = "M5NR8PT-DWX44KT-HBT1WVC-CE0GP91"
INPUT_FILE = "movies_updated.csv"
OUTPUT_FILE = "movies_updated.csv"

url = "https://api.kinopoisk.dev/v1.4/movie"
headers = {"X-API-KEY": API_KEY}

movies = []
with open(INPUT_FILE, encoding="utf-8-sig") as f:
    reader = csv.DictReader(f)
    for row in reader:
        row.setdefault("rating_kp", "0")
        row.setdefault("rating_imdb", "0")
        row.setdefault("votes_imdb", "0")
        movies.append(row)

print(f"Всего фильмов: {len(movies)}")

to_update = [m for m in movies if float(m.get("rating_imdb") or 0) == 0]
print(f"Нужно обновить рейтинг: {len(to_update)} фильмов")


def fetch_rating(title, year, retries=5):
    search_url = "https://api.kinopoisk.dev/v1.4/movie/search"
    params = (
        ("query", title),
        ("year", year),
        ("limit", 5),
        ("type", "movie"),
    )
    for attempt in range(1, retries + 1):
        try:
            response = requests.get(search_url, headers=headers, params=params, timeout=10)

            if response.status_code == 200:
                docs = response.json().get("docs", [])
                for doc in docs:
                    if str(doc.get("year")) == str(year):
                        rating = doc.get("rating", {})
                        votes = doc.get("votes", {})
                        return {
                            "rating_kp": str(round(rating.get("kp") or 0, 1)),
                            "rating_imdb": str(round(rating.get("imdb") or 0, 1)),
                            "votes_imdb": str(votes.get("imdb") or 0),
                        }
                if docs:
                    doc = docs[0]
                    rating = doc.get("rating", {})
                    votes = doc.get("votes", {})
                    return {
                        "rating_kp": str(round(rating.get("kp") or 0, 1)),
                        "rating_imdb": str(round(rating.get("imdb") or 0, 1)),
                        "votes_imdb": str(votes.get("imdb") or 0),
                    }
                return None

            elif response.status_code == 403:
                # Лимит запросов — ждём и пробуем снова
                wait = 30 * attempt
                print(f"  ⚠ 403 Лимит запросов, ждём {wait}с (попытка {attempt}/{retries})...")
                time.sleep(wait)

            elif response.status_code == 429:
                wait = 15 * attempt
                print(f"  ⚠ 429 Rate limit, ждём {wait}с...")
                time.sleep(wait)

            elif response.status_code in (502, 503, 504):
                wait = 5 * attempt
                print(f"  ⚠ Сервер недоступен ({response.status_code}), попытка {attempt}/{retries}...")
                time.sleep(wait)

            else:
                print(f"  ✗ Ошибка API: {response.status_code}")
                return None

        except requests.exceptions.Timeout:
            print(f"  ⚠ Таймаут, попытка {attempt}/{retries}...")
            time.sleep(5 * attempt)
        except requests.exceptions.ConnectionError:
            print(f"  ⚠ Ошибка соединения, попытка {attempt}/{retries}...")
            time.sleep(5 * attempt)

    print(f"  ✗ Не удалось получить рейтинг после {retries} попыток")
    return None


def save_all():
    movies_sorted = sorted(movies, key=lambda x: float(x.get("rating_imdb") or 0), reverse=True)
    for j, m in enumerate(movies_sorted):
        m["rank"] = str(j + 1)
    with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
        fieldnames = ["rank", "title", "year", "country", "genres", "description", "rating_kp", "rating_imdb", "votes_imdb"]
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(movies_sorted)


updated = 0
failed = 0

for i, movie in enumerate(to_update):
    title = movie["title"]
    year = movie["year"]

    result = fetch_rating(title, year)

    if result:
        movie["rating_kp"] = result["rating_kp"]
        movie["rating_imdb"] = result["rating_imdb"]
        movie["votes_imdb"] = result["votes_imdb"]
        updated += 1
        print(f"[{i+1}/{len(to_update)}] ✓ {title} ({year}) | KP: {result['rating_kp']} | IMDb: {result['rating_imdb']}")
    else:
        failed += 1
        print(f"[{i+1}/{len(to_update)}] ✗ Не найден: {title} ({year})")

    if (i + 1) % 50 == 0:
        save_all()
        print(f"  💾 Прогресс сохранён (обновлено: {updated}, не найдено: {failed})")

    # Задержка между запросами — ключ к тому чтобы не словить 403
    time.sleep(1.0)

save_all()
print(f"\nГотово!")
print(f"  Обновлено: {updated} фильмов")
print(f"  Не найдено: {failed} фильмов")

movies_sorted = sorted(movies, key=lambda x: float(x.get("rating_imdb") or 0), reverse=True)
print(f"\nТоп-5 по IMDb:")
for m in movies_sorted[:5]:
    print(f"  {m['rank']}. {m['title']} ({m['year']}) — IMDb: {m['rating_imdb']}")