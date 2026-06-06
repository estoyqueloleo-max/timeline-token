from gliner2 import GLiNER2
import json

labels = [
    "trabaja en", "fundó", "adquirió", "es subsidiaria de", "invirtió en",
    "se asoció con", "es competencia de", "dimitió de", "fue despedido de", "firmó un contrato con",
    "es proveedor de", "lanzó el producto", "anunció ganancias de", "fue multada por", "patrocina a",
    "ubicado en", "nació en", "visitó", "declaró la guerra a", "firmó un tratado con",
    "impuso sanciones a", "es aliado de", "votó a favor de", "vetó", "se independizó de",
    "demandó a", "fue arrestado por", "fue condenado por", "es investigado por", "asesinó a",
    "robó a", "testificó contra", "fue absuelto de", "está casado con", "es familiar de",
    "criticó a", "apoyó a", "se reunió con", "falleció en", "donó a",
    "se divorció de", "premió a", "descubrió", "publicó", "escribió",
    "dirigió", "protagonizó", "desarrolló", "ganó el premio", "investiga sobre"
]

try:
    model = GLiNER2.from_pretrained("fastino/gliner2-base-v1")
    schema = {"relations": [{lbl: {"head": "", "tail": ""}} for lbl in labels]}
    r1 = model.processor.transform_and_format("Texto uno", schema)
    p1 = r1.input_ids[:r1.input_ids.index(128002)+1]
    
    with open("prompt_ids.json", "w") as f:
        json.dump({"prompt_ids": p1, "labels": labels}, f)
    print("SUCCESS")
except Exception as e:
    print("ERROR:", e)
