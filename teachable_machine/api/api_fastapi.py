"""API на FastAPI.  Запуск: python api_fastapi.py   (документация: http://127.0.0.1:8000/docs)
Запрос: POST /predict, в теле - байты картинки."""

from classifier import classify
from fastapi import FastAPI, HTTPException, Request

app = FastAPI(title="Flowers API")

# Это нужно только чтобы в /docs появилась кнопка выбора файла
BINARY_BODY = {
    "requestBody": {
        "required": True,
        "content": {
            "application/octet-stream": {
                "schema": {"type": "string", "format": "binary"}
            }
        },
    }
}


@app.post("/predict", openapi_extra=BINARY_BODY)
async def predict(request: Request):
    data = await request.body()
    if not data:
        raise HTTPException(400, "пустой запрос: отправь байты картинки в теле")
    try:
        return classify(data)
    except ValueError as e:
        raise HTTPException(400, str(e))


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, port=8000)
