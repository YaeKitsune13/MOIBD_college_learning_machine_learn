from tensorflow.keras.datasets import mnist

# Библиотека сама скачает файлы (около 11 МБ) при первом запуске
(X_train, y_train), (X_test, y_test) = mnist.load_data()

print(f"Загружено изображений для обучения: {X_train.shape[0]}")  # 60000
print(f"Загружено изображений для теста: {X_test.shape[0]}")      # 10000
# Каждое изображение уже имеет матрицу 28x28 пикселей
