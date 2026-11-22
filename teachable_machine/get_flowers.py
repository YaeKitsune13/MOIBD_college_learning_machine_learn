import os
import random
import shutil

SRC = "flowers"
CLASSES = ["daisy", "rose", "sunflower"]
N_TRAIN, N_VAL = 40, 15
random.seed(42)

for out in ("flowers_train", "flowers_validation"):
    shutil.rmtree(out, ignore_errors=True)

for cls in CLASSES:
    folder = os.path.join(SRC, cls)
    files = [
        f for f in os.listdir(folder) if f.lower().endswith((".jpg", ".jpeg", ".png"))
    ]
    random.shuffle(files)
    parts = {
        "flowers_train": files[:N_TRAIN],
        "flowers_validation": files[N_TRAIN : N_TRAIN + N_VAL],
    }
    for out, chosen in parts.items():
        os.makedirs(f"{out}/{cls}", exist_ok=True)
        for f in chosen:
            shutil.copy(os.path.join(folder, f), f"{out}/{cls}/{f}")
    print(
        cls,
        "->",
        len(parts["flowers_train"]),
        "train,",
        len(parts["flowers_validation"]),
        "val",
    )
