import json
import re

with open('prompt_ids.json', 'r') as f:
    new_json = f.read().strip()

with open('analysis.go', 'r') as f:
    content = f.read()

pattern = r'defaultPrompt := `.*?`'
replacement = f'defaultPrompt := `{new_json}`'

# Using a function to avoid parsing escape characters in the replacement string
new_content = re.sub(pattern, lambda _: replacement, content, flags=re.DOTALL)

with open('analysis.go', 'w') as f:
    f.write(new_content)

print("Successfully injected 50 labels into analysis.go")
