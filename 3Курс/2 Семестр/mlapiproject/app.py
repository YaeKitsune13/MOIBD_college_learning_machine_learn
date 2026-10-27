import os

import requests
import streamlit as st

os.environ["HTTP_PROXY"] = ""
os.environ["HTTPS_PROXY"] = ""

st.title("Предсказание кластера фильма")

model_choice = st.selectbox(
    "Выберите модель",
    [
        "random_forest",
        "gradient_boosting",
        "k-nearest_neighbors",
        "pytorch_nn",
    ],
)

input_text = st.text_area("Введите описание фильма", height=200)

if st.button("Предсказать"):
    if input_text == "":
        st.warning("Введите текст")
    else:
        response = requests.post(
            "http://127.0.0.1:8081/predict",
            json={"text": input_text, "model": model_choice},
        )
        result = response.json()
        st.markdown("#### Предсказанный кластер")
        st.write(result.get("cluster"))
