import pandas as pd
from deep_translator import GoogleTranslator
import time

df = pd.read_csv('imdb_results.csv')
translator = GoogleTranslator(source='en', target='ru')

def translate_safe(text, index, field):
    try:
        result = translator.translate(str(text))
        print(f"[{index}] {field}: {str(text)[:50]}... → {result[:50]}...")
        time.sleep(0.3)
        return result
    except Exception as e:
        print(f"Ошибка на строке {index} ({field}): {e}")
        return text

translated_desc = []
translated_genres = []

for i, row in df.iterrows():
    translated_desc.append(translate_safe(row['description'], i, 'description'))
    translated_genres.append(translate_safe(row['genres'], i, 'genres'))

df['description'] = translated_desc
df['genres'] = translated_genres

df.to_csv('movies_ru.csv', index=False, encoding='utf-8-sig')
print("Готово!")
