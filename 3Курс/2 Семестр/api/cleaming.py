import csv
import re

INPUT_FILE = "/home/yaekitsune/Projects/MOiBD/movies_updated.csv"
OUTPUT_FILE = "movies_final_cleaned.csv"

def normalize_title(title):
    if not title: return ""
    t = title.lower()
    t = t.replace('ё', 'е')
    t = re.sub(r'[^\w\s]', '', t)
    return " ".join(t.split())

def clean_movies():
    # Используем словарь, где ключом будет название фильма
    # Это автоматически схлопнет все одинаковые названия в одну запись
    unique_movies = {}
    
    total_input = 0
    
    try:
        with open(INPUT_FILE, encoding="utf-8-sig") as f:
            reader = csv.DictReader(f)
            for row in reader:
                total_input += 1
                title = row.get("title", "").strip()
                if not title:
                    continue
                
                norm_title = normalize_title(title)
                
                # Получаем текущие показатели фильма для сравнения
                try:
                    current_votes = int(row.get("votes_imdb") or 0)
                    current_rating = float(row.get("rating_imdb") or 0)
                except ValueError:
                    current_votes = 0
                    current_rating = 0

                if norm_title not in unique_movies:
                    # Если такого фильма еще нет, добавляем его
                    unique_movies[norm_title] = row
                else:
                    # Если фильм уже есть, выбираем лучший вариант из двух
                    existing_row = unique_movies[norm_title]
                    try:
                        existing_votes = int(existing_row.get("votes_imdb") or 0)
                    except ValueError:
                        existing_votes = 0
                    
                    # ПРАВИЛО: Оставляем ту запись, где больше голосов IMDb 
                    # (обычно у "правильного" дубликата голосов в разы больше)
                    if current_votes > existing_votes:
                        unique_movies[norm_title] = row
                    elif current_votes == existing_votes and current_rating > 0:
                        # Если голосов поровну (или 0), смотрим на рейтинг
                        unique_movies[norm_title] = row

        # Превращаем словарь обратно в список
        cleaned_list = list(unique_movies.values())

        # Сортируем весь список по рейтингу IMDb (от лучшего к худшему)
        def sort_key(x):
            try:
                return float(x.get("rating_imdb") or 0)
            except:
                return 0.0

        cleaned_list.sort(key=sort_key, reverse=True)

        # Пересчитываем ранги (1, 2, 3...)
        for i, movie in enumerate(cleaned_list):
            movie["rank"] = str(i + 1)

        # Сохраняем результат
        fieldnames = ["rank", "title", "year", "country", "genres", "description", "rating_kp", "rating_imdb", "votes_imdb"]
        with open(OUTPUT_FILE, "w", newline="", encoding="utf-8") as f:
            writer = csv.DictWriter(f, fieldnames=fieldnames)
            writer.writeheader()
            writer.writerows(cleaned_list)

        print(f"Очистка завершена успешно!")
        print(f"Было строк: {total_input}")
        print(f"Стало уникальных фильмов: {len(cleaned_list)}")
        print(f"Удалено дублей: {total_input - len(cleaned_list)}")
        print(f"Результат в файле: {OUTPUT_FILE}")

    except Exception as e:
        print(f"Произошла ошибка: {e}")

if __name__ == "__main__":
    clean_movies()