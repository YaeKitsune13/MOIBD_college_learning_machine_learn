"""Окно: выбрать фото -> отправить в API -> показать класс и точность.
Сначала запусти API (api_flask.py или api_fastapi.py), потом: python client.py"""
import json
import tkinter as tk
import urllib.error
import urllib.request
from tkinter import filedialog

from PIL import Image, ImageTk

API_URL = "http://127.0.0.1:8000/predict"


def ask_api(path):
    with open(path, "rb") as f:
        data = f.read()
    req = urllib.request.Request(API_URL, data=data, headers={"Content-Type": "application/octet-stream"})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise RuntimeError(json.load(e).get("detail", "ошибка API"))
    except urllib.error.URLError:
        raise RuntimeError("API не отвечает. Он запущен?")


def choose_photo():
    path = filedialog.askopenfilename(filetypes=[("Картинки", "*.jpg *.jpeg *.png *.webp")])
    if not path:
        return
    img = Image.open(path)
    img.thumbnail((400, 400))
    photo = ImageTk.PhotoImage(img)
    image_label.config(image=photo)
    image_label.image = photo  # иначе tkinter выкинет картинку

    result_label.config(text="Думаю...", fg="black")
    root.update()
    try:
        r = ask_api(path)
        result_label.config(text=f"{r['class']} - {r['confidence'] * 100:.1f}%", fg="green")
    except RuntimeError as e:
        result_label.config(text=str(e), fg="red")


root = tk.Tk()
root.title("Распознавание цветов")
tk.Button(root, text="Выбрать фото", command=choose_photo).pack(padx=20, pady=10)
image_label = tk.Label(root)
image_label.pack()
result_label = tk.Label(root, text="", font=("sans-serif", 16))
result_label.pack(padx=20, pady=10)
root.mainloop()
