import os

FOLDER = "pdf_articles"

def remove_duplicates():
    # Словарь для хранения { 'название_без_номера': 'полное_имя_файла' }
    seen_names = {}
    removed = 0

    # Сортируем, чтобы файлы с меньшими номерами (56_) шли первыми
    files = sorted(os.listdir(FOLDER))

    for file in files:
        if not file.endswith(".pdf"):
            continue

        # Отрезаем префикс до первого "_"
        # "57_Статья.pdf" -> "Статья.pdf"
        if "_" in file:
            clean_name = file.split("_", 1)[1]
        else:
            clean_name = file

        path = os.path.join(FOLDER, file)

        if clean_name in seen_names:
            # Если мы уже видели такое название, удаляем текущий файл
            os.remove(path)
            print(f"Удалён дубликат: {file}")
            print(f"Оставлен оригинал: {seen_names[clean_name]}")
            removed += 1
        else:
            # Запоминаем, что такое название нам встретилось
            seen_names[clean_name] = file

    print(f"\nГотово. Удалено дубликатов по названию: {removed}")

if __name__ == "__main__":
    # Проверьте, что путь к папке верный относительно скрипта
    if os.path.exists(FOLDER):
        remove_duplicates()
    else:
        print(f"Ошибка: Папка '{FOLDER}' не найдена!")
