# Customer Service AI Agent (RAG + Skills + ElevenLabs)

This guide provides a streamlined Python implementation for a voice-enabled customer service agent. It combines **Retrieval-Augmented Generation (RAG)** for company policies, **Agent Skills (Function Calling)** for live database lookups, and **ElevenLabs** for natural text-to-speech (TTS) replies.

---

## 1. Prerequisites & Setup

Install the official Google GenAI and ElevenLabs SDKs:

```bash
pip install google-genai elevenlabs
```

Set your API keys as environment variables:

```bash
export GEMINI_API_KEY="your-gemini-api-key"
export ELEVENLABS_API_KEY="your-elevenlabs-api-key"
```

---

## 2. Complete Python Implementation

Save the following code as `agent.py`. When executed, it processes a customer query that requires both a policy lookup (RAG) and a live order status check (Agent Skill), then saves the spoken reply as `reply.mp3`.

```python
import os
from elevenlabs.client import ElevenLabs
from google import genai
from google.genai import types

# 1. Initialize Clients
gemini_client = genai.Client()
eleven_client = ElevenLabs(api_key=os.environ.get("ELEVENLABS_API_KEY"))

# ==========================================
# COMPONENT 1: Simple RAG (Knowledge Base)
# ==========================================
KNOWLEDGE_BASE = {
    "return_policy": (
        "Customers can return any unworn item within 30 days of delivery for a"
        " full refund. Return shipping is free for defective items; otherwise,"
        " a $5 fee applies."
    ),
    "shipping_times": (
        "Standard shipping takes 3-5 business days. Express shipping takes 1-2"
        " business days."
    ),
    "support_hours": (
        "Live human agents are available Monday through Friday, 9 AM to 6 PM"
        " EST."
    ),
}


def retrieve_context(query: str) -> str:
    """Simple keyword-based retrieval (replace with a Vector DB in production)."""
    query_lower = query.lower()
    relevant_docs = []

    if any(word in query_lower for word in ["return", "refund", "send back"]):
        relevant_docs.append(KNOWLEDGE_BASE["return_policy"])
    if any(word in query_lower for word in ["ship", "delivery", "arrive"]):
        relevant_docs.append(KNOWLEDGE_BASE["shipping_times"])
    if any(word in query_lower for word in ["hour", "human", "contact"]):
        relevant_docs.append(KNOWLEDGE_BASE["support_hours"])

    return (
        "\n".join(relevant_docs)
        if relevant_docs
        else "No specific policy found."
    )


# ==========================================
# COMPONENT 2: Agent Skill (Tool / Function)
# ==========================================
def check_order_status(order_id: str) -> str:
    """Checks the real-time shipping status of a customer's order.

    Args:
        order_id: The unique order identifier (e.g., 'ORD-123').
    """
    mock_orders = {
        "ORD-123": (
            "Shipped via Express. Expected delivery: Tomorrow by 8:00 PM."
        ),
        "ORD-456": "Processing in warehouse. Expected to ship within 24 hours.",
    }
    return mock_orders.get(
        order_id.upper(), f"Order {order_id} was not found in our system."
    )


# ==========================================
# COMPONENT 3: Customer Service Agent & TTS
# ==========================================
def handle_customer_query(
    user_message: str, output_audio_file: str = "reply.mp3"
):
    print(f"Customer: {user_message}\n")

    # Step A: Retrieve RAG context
    rag_context = retrieve_context(user_message)

    # Step B: Configure Persona, RAG Context, and Skills
    system_instruction = f"""
    You are a warm, helpful, and concise customer service representative for 'Acme Store'.
    - Keep answers conversational and under 3 sentences, as your reply will be spoken aloud via text-to-speech.
    - Avoid using markdown formatting, bullet points, or asterisks.
    - Use the provided Company Policy Context to answer policy questions accurately.
    - Use the `check_order_status` tool whenever a user asks about a specific order.
    
    Company Policy Context:
    {rag_context}
    """

    # Step C: Generate AI Response (Gemini automatically handles calling the tool)
    response = gemini_client.models.generate_content(
        model="gemini-2.5-flash",
        contents=user_message,
        config=types.GenerateContentConfig(
            system_instruction=system_instruction,
            tools=[check_order_status],
            temperature=0.3,
        ),
    )

    ai_reply_text = response.text
    print(f"Agent (Text): {ai_reply_text}\n")

    # Step D: Convert AI Reply to Speech using ElevenLabs
    print("Generating voice response...")
    audio_generator = eleven_client.text_to_speech.convert(
        text=ai_reply_text,
        voice_id="21m00Tcm4TlvDq8ikWAM",  # 'Rachel' voice
        model_id="eleven_flash_v2_5",  # Low-latency model for conversational AI
        output_format="mp3_44100_128",
    )

    with open(output_audio_file, "wb") as f:
        for chunk in audio_generator:
            if chunk:
                f.write(chunk)

    print(f"Saved audio reply to {output_audio_file}")


if __name__ == "__main__":
    sample_query = (
        "Hi, can you check on order ORD-123? Also, if it doesn't fit, how long"
        " do I have to return it?"
    )
    handle_customer_query(sample_query)
```

---

## 3. Architecture Summary

| Component | Implementation | Purpose |
| :--- | :--- | :--- |
| **RAG Context** | `retrieve_context()` | Injects static company policies into the system prompt so the agent answers accurately without hallucinating. |
| **Agent Skill** | `check_order_status()` | Allows Gemini to dynamically query external databases or APIs when a user provides an order ID. |
| **Voice Output** | `eleven_flash_v2_5` | Synthesizes natural, ultra-low-latency audio suitable for real-time customer support interactions. |