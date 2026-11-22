import json
import urllib.error
import urllib.request

import streamlit as st

API_URL = "http://127.0.0.1:8000/predict"


@st.cache_data
def ask_api(data: bytes):
    req = urllib.request.Request(
        API_URL, data=data, headers={"Content-Type": "application/octet-stream"}
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise RuntimeError(json.load(e).get("detail", "ошибка API"))
    except urllib.error.URLError:
        raise RuntimeError("API не отвечает. Он запущен?")


st.title("Распознавание цветов")
file = st.file_uploader("Выбери фото", type=["jpg", "jpeg", "png", "webp"])

if file is not None:
    data = file.getvalue()
    st.image(data, width=400)
    try:
        with st.spinner("Думаю..."):
            r = ask_api(data)
        st.success(f"{r['class']} - {r['confidence'] * 100:.1f}%")
    except RuntimeError as e:
        st.error(str(e))
