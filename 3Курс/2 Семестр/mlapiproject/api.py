import re
import string

import joblib
import nltk
import numpy as np
import pymorphy3
import torch
import torch.nn as nn
import uvicorn
from fastapi import FastAPI
from nltk.corpus import stopwords
from nltk.tokenize import word_tokenize
from pydantic import BaseModel

nltk.download("punkt_tab", quiet=True)
nltk.download("punkt", quiet=True)
nltk.download("stopwords", quiet=True)
stop_words = set(stopwords.words("russian"))
morph = pymorphy3.MorphAnalyzer()

app = FastAPI()


class MovieClassifier(nn.Module):
    def __init__(self, input_dim, num_classes):
        super(MovieClassifier, self).__init__()
        self.network = nn.Sequential(
            nn.Linear(input_dim, 512),
            nn.BatchNorm1d(512),
            nn.ReLU(),
            nn.Dropout(0.4),
            nn.Linear(512, 256),
            nn.BatchNorm1d(256),
            nn.ReLU(),
            nn.Dropout(0.4),
            nn.Linear(256, 128),
            nn.BatchNorm1d(128),
            nn.ReLU(),
            nn.Dropout(0.3),
            nn.Linear(128, num_classes),
        )

    def forward(self, x):
        return self.network(x)


class Item(BaseModel):
    text: str
    model: str = "random_forest"


device = torch.device("cpu")

try:
    models = {
        "random_forest": joblib.load("./model_random_forest.pkl"),
        "gradient_boosting": joblib.load("./model_gradient_boosting.pkl"),
        "k-nearest_neighbors": joblib.load("./model_k-nearest_neighbors.pkl"),
    }
    vectorizer = joblib.load("./vectorizer.pkl")
    scaler = joblib.load("./scaler.pkl")

    input_dim = vectorizer.transform([""]).shape[1]
    nn_model = MovieClassifier(input_dim=input_dim, num_classes=4).to(device)
    nn_model.load_state_dict(torch.load("./model_pytorch_nn.pth", map_location=device))
    nn_model.eval()
    models["pytorch_nn"] = nn_model

except FileNotFoundError as e:
    print(f"Ошибка: {e}")


def fun_punctuation_text(text):
    text = text.lower()
    text = re.sub(f"[{re.escape(string.punctuation)}]", "", text)
    text = re.sub(r"\d", "", text)
    text = re.sub(r"[^а-яё\s]", " ", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def fun_lemmentization_text(text):
    tokens = word_tokenize(text)
    return " ".join(morph.parse(w)[0].normal_form for w in tokens)


def fun_tokenize(text):
    tokens = word_tokenize(text)
    return " ".join(t for t in tokens if t not in stop_words)


def fun_pred_text(text):
    text = fun_punctuation_text(text)
    text = fun_lemmentization_text(text)
    text = fun_tokenize(text)
    return text


mapping = {
    0: "Драма, отношения, семья",
    1: "Война, история, сражение",
    2: "Преступление, детектив, расследование",
    3: "Приключения, фэнтези, магия",
}


def predict_cluster(text, model_name="random_forest"):
    processed_text = fun_pred_text(text)
    text_vectorized = vectorizer.transform([processed_text])

    if model_name == "pytorch_nn":
        text_scaled = scaler.transform(text_vectorized.toarray())
        tensor = torch.FloatTensor(text_scaled).to(device)
        with torch.no_grad():
            output = nn_model(tensor)
            probabilities = torch.softmax(output, dim=1).cpu().numpy().tolist()
            prediction = int(torch.argmax(output, dim=1).item())
    else:
        model = models.get(model_name, models["random_forest"])
        prediction = int(model.predict(text_vectorized)[0])  # ← исправлено
        probabilities = model.predict_proba(text_vectorized).tolist()

    return {
        "cluster": mapping.get(prediction, "Неизвестный кластер"),
        "class_id": prediction,
        "probabilities": probabilities,
    }


@app.post("/predict")
async def post_pred_text(item: Item):
    return predict_cluster(item.text, item.model)


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8081)
