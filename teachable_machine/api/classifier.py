"""Модель Teachable Machine + функция classify().
Код загрузки взят из рабочего ноутбука (обход несовместимости с Keras 3)."""

import io
import json
import os

os.environ["TF_CPP_MIN_LOG_LEVEL"] = "3"

from contextlib import contextmanager

import h5py
import keras
import keras.src.layers.layer as keras_layer_module
import numpy as np
from keras.layers import GlobalAveragePooling2D
from keras.src.legacy.saving import legacy_h5_format, saving_utils
from PIL import Image, ImageOps, UnidentifiedImageError

BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MODEL_PATH = os.path.join(BASE, "models", "flowers_model.h5")
LABELS_PATH = os.path.join(BASE, "models", "flowers_labels.txt")


@contextmanager
def pick_vector_from_pair():
    """Пока собирается модель: если слою пришла пара тензоров, берём один нужного ранга
    (карту признаков при необходимости сжимаем GlobalAveragePooling2D)."""
    orig_call = keras_layer_module.Layer.__call__

    def patched(self, *args, **kwargs):
        inputs = getattr(self, "inputs", None)
        if (
            isinstance(inputs, list)
            and len(inputs) == 1
            and len(args) == 1
            and isinstance(args[0], (list, tuple))
            and len(args[0]) == 2
        ):
            want_rank = len(inputs[0].shape)
            same_rank = [t for t in args[0] if len(t.shape) == want_rank]
            if same_rank:
                t = same_rank[0]
            else:
                t = args[0][0]
                if want_rank == 2 and len(t.shape) == 4:
                    t = GlobalAveragePooling2D()(t)
            args = (t,)
        return orig_call(self, *args, **kwargs)

    keras_layer_module.Layer.__call__ = patched
    try:
        yield
    finally:
        keras_layer_module.Layer.__call__ = orig_call


def clean_config(obj):
    """Убираем 'groups' (его не понимает DepthwiseConv2D в новом Keras)."""
    if isinstance(obj, dict):
        obj.pop("groups", None)
        for v in obj.values():
            clean_config(v)
    elif isinstance(obj, list):
        for item in obj:
            clean_config(item)


def load_tm_model(h5_path):
    with h5py.File(h5_path, "r") as f:
        model_config = f.attrs.get("model_config")
        if isinstance(model_config, bytes):
            model_config = model_config.decode("utf-8")
        config_dict = json.loads(model_config)
        clean_config(config_dict)
        with pick_vector_from_pair():
            model = saving_utils.model_from_config(config_dict)
        legacy_h5_format.load_weights_from_hdf5_group(f["model_weights"], model)
    return model


def iter_leaf_layers(layers_list):
    """Разворачиваем вложенные Sequential."""
    for layer in layers_list:
        if isinstance(layer, keras.Sequential):
            yield from iter_leaf_layers(layer.layers)
        else:
            yield layer


def build_inference_model(loaded):
    """Чистый граф Input -> слои -> выход (веса те же)."""
    inp = keras.Input(shape=(224, 224, 3))
    x = inp
    for layer in iter_leaf_layers(loaded.layers):
        if isinstance(x, (list, tuple)):
            x = x[0]
        if isinstance(layer, keras.layers.Dense):
            want_rank = 2
        else:
            try:
                want_rank = len(layer.inputs[0].shape)
            except Exception:
                want_rank = None
        if want_rank == 2 and len(x.shape) == 4:
            x = GlobalAveragePooling2D()(x)
        x = layer(x)
    if isinstance(x, (list, tuple)):
        x = x[0]
    return keras.Model(inp, x)


# Модель грузится один раз при запуске сервера
model = build_inference_model(load_tm_model(MODEL_PATH))
with open(LABELS_PATH, "r", encoding="utf-8") as f:
    class_names = [line.strip() for line in f if line.strip()]


def classify(image_bytes):
    """Принимает байты картинки, возвращает {'index', 'class', 'confidence'}."""
    try:
        image = Image.open(io.BytesIO(image_bytes)).convert("RGB")
    except (UnidentifiedImageError, OSError):
        raise ValueError("это не картинка (или формат не поддерживается)")
    image = ImageOps.fit(image, (224, 224), Image.Resampling.LANCZOS)
    data = (np.asarray(image).astype(np.float32) / 127.5 - 1)[np.newaxis]

    prediction = model.predict(data, verbose=0)[0]
    index = int(np.argmax(prediction))
    name = class_names[index]
    return {
        "index": index,
        "class": name.split(" ", 1)[1] if " " in name else name,
        "confidence": round(float(prediction[index]), 4),
    }
