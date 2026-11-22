"""API на Flask.  Запуск: python api_flask.py
Запрос: POST /predict, в теле - байты картинки."""
from flask import Flask, jsonify, request

from classifier import classify

app = Flask(__name__)
app.json.ensure_ascii = False  # чтобы русские названия не превращались в \uXXXX


@app.post("/predict")
def predict():
    data = request.get_data()
    if not data:
        return jsonify(detail="пустой запрос: отправь байты картинки в теле"), 400
    try:
        return jsonify(classify(data))
    except ValueError as e:
        return jsonify(detail=str(e)), 400


if __name__ == "__main__":
    app.run(port=8000)
