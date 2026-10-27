import pandas as pd
from deep_translator import GoogleTranslator
import time
from math import ceil

# Чтение данных
df = pd.read_csv('imdb_results.csv')

translator = GoogleTranslator(source='en', target='ru')

def safe_translate(text, index, field):
    """Перевод с повторными попытками и обработкой NaN."""
    if pd.isna(text) or str(text).strip() == '':
        return ''
    text_str = str(text)
    max_retries = 3
    for attempt in range(max_retries):
        try:
            result = translator.translate(text_str)
            print(f"[{index}] {field}: {text_str[:50]}... → {result[:50]}...")
            return result
        except Exception as e:
            print(f"⚠️ Попытка {attempt+1} не удалась для строки {index} ({field}): {e}")
            if attempt < max_retries - 1:
                time.sleep(5)  # ждём подольше при ошибке
            else:
                print(f"❌ Не удалось перевести '{text_str[:30]}...' после {max_retries} попыток.")
                return text_str  # возвращаем оригинал

# Перевод с сохранением промежуточных результатов каждые 20 строк
batch_size = 20
total_rows = len(df)

for start in range(0, total_rows, batch_size):
    end = min(start + batch_size, total_rows)
    print(f"\n📦 Обработка строк {start}–{end-1} из {total_rows-1}")
    
    for i in range(start, end):
        row = df.iloc[i]
        # Перевод описания и жанров
        df.at[i, 'description'] = safe_translate(row['description'], i, 'description')
        time.sleep(0.2)
        df.at[i, 'genres'] = safe_translate(row['genres'], i, 'genres')
        time.sleep(0.2)
    
    # Сохраняем частичный результат
    df.to_csv('movies_ru_temp.csv', index=False, encoding='utf-8-sig')
    print(f"💾 Сохранён промежуточный результат в movies_ru_temp.csv")

# Финальное сохранение
df.to_csv('movies_ru.csv', index=False, encoding='utf-8-sig')
print("\n✅ Готово! Результат сохранён в movies_ru.csv")