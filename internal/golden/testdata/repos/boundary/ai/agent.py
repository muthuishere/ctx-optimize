import os
from openai import OpenAI

client = OpenAI(api_key=os.environ["OPENAI_API_KEY"])


def summarize(text):
    return client.chat.completions.create(
        model="gpt-4o-mini",
        messages=[{"role": "user", "content": text}],
    )
