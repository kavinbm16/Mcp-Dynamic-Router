import asyncio
import json
import websockets
import aiohttp

# Configurations for OpenAI Realtime and the Dynamic Router sidecar
OPENAI_WS_URL = "wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview"
OPENAI_API_KEY = "your-openai-api-key"
SIDECAR_URL = "http://127.0.0.1:8090"

async def run_openai_realtime_session():
    headers = {
        "Authorization": f"Bearer {OPENAI_API_KEY}",
        "OpenAI-Beta": "realtime=2024-10-01"
    }

    # Generate a unique session ID for the dynamic router Stream RAG state machine
    router_session_id = "openai-session-123"

    async with websockets.connect(OPENAI_WS_URL, extra_headers=headers) as ws:
        print("Connected to OpenAI Realtime API")

        # 1. Initialize the session and register the meta-router tool with OpenAI
        session_update = {
            "type": "session.update",
            "session": {
                "modalities": ["audio", "text"],
                "instructions": "You are a helpful assistant. If you need information you do not have, construct an intent query and call the 'invoke_tool' function.",
                "tools": [
                    {
                        "type": "function",
                        "name": "invoke_tool",
                        "description": "Semantic Tool Router. Invokes specialized MCP tools (weather, calculations, search) by matching the intent query.",
                        "parameters": {
                            "type": "object",
                            "properties": {
                                "intent": {
                                    "type": "string",
                                    "description": "The user query or intent description (e.g. 'weather forecast in Bengaluru')."
                                },
                                "arguments": {
                                    "type": "object",
                                    "description": "Parameters extracted from the conversation matching the tool requirement (e.g., {'location': 'Bengaluru'})."
                                }
                            },
                            "required": ["intent", "arguments"]
                        }
                    }
                ]
            }
        }
        await ws.send(json.dumps(session_update))

        # Event loop to handle incoming OpenAI realtime ws messages
        async for raw_message in ws:
            event = json.loads(raw_message)
            event_type = event.get("type")

            # 2. Intercept speech transcriptions (ASR partials) and pipe them to Stream RAG
            if event_type == "conversation.item.input_audio_transcription.completed":
                # Realtime transcript of the user's audio input is complete
                transcript = event.get("transcript", "")
                await send_to_stream_rag(router_session_id, transcript, is_final=True)

            elif event_type == "response.function_call_arguments.done":
                # OpenAI LLM called 'invoke_tool'. We intercept the call and execute it via the router.
                await handle_tool_call(ws, event)

async def send_to_stream_rag(session_id, transcript, is_final):
    """
    Pipes transcripts to the router sidecar. This can also be called incrementally
    with is_final=False during partial ASR updates.
    """
    payload = {
        "session_id": session_id,
        "transcript": transcript,
        "final": is_final
    }
    url = f"{SIDECAR_URL}/v1/stream"
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(url, json=payload) as resp:
                if resp.status == 200:
                    data = await resp.json()
                    # If stable, we can prefetch resources
                    if data.get("stable") and not data.get("final"):
                        candidates = data["result"].get("candidates", [])
                        if candidates:
                            print(f"[Prefetch] Warming connection for: {candidates[0]['tool']['id']}")
    except Exception as e:
        print(f"Error calling Stream RAG: {e}")

async def handle_tool_call(ws, event):
    """
    Resolves the intent via the router and executes the tool on the dynamic router gateway.
    """
    call_id = event.get("call_id")
    tool_name = event.get("name")
    
    if tool_name != "invoke_tool":
        return

    # Parse arguments provided by the LLM
    args = json.loads(event.get("arguments", "{}"))
    intent = args.get("intent")
    tool_args = args.get("arguments", {})

    print(f"[OpenAI Tool Call] Intercepted intent: {intent}")

    # 1. Query the router to find the best candidate tool
    route_payload = {
        "utterance": intent,
        "final": True
    }
    
    async with aiohttp.ClientSession() as session:
        # Route
        async with session.post(f"{SIDECAR_URL}/v1/route", json=route_payload) as resp:
            if resp.status != 200:
                print("Routing failed")
                return
            route_data = await resp.json()
            
        if route_data.get("decision") != "selected":
            print(f"Abstained: {route_data.get('reason')}")
            # Send failure back to OpenAI
            await send_tool_output(ws, call_id, f"Error: {route_data.get('reason')}")
            return
            
        candidate_tool = route_data["candidates"][0]["tool"]
        print(f"[Router Selected] ID: {candidate_tool['id']}")

        # 2. Execute the tool via /v1/execute on the sidecar
        exec_payload = {
            "tool_id": candidate_tool["id"],
            "arguments": tool_args
        }
        
        async with session.post(f"{SIDECAR_URL}/v1/execute", json=exec_payload) as resp:
            if resp.status != 200:
                err_data = await resp.json()
                await send_tool_output(ws, call_id, f"Execution failed: {err_data.get('error')}")
                return
            exec_result = await resp.json()
            
            # Send the final tool outputs back to the OpenAI session
            print(f"[Execution Success] Output: {exec_result.get('content')}")
            await send_tool_output(ws, call_id, json.dumps(exec_result.get("content")))

async def send_tool_output(ws, call_id, output_text):
    """Sends function outputs back to the OpenAI session to continue response generation."""
    response = {
        "type": "conversation.item.create",
        "item": {
            "type": "function_call_output",
            "call_id": call_id,
            "output": output_text
        }
    }
    await ws.send(json.dumps(response))
    await ws.send(json.dumps({"type": "response.create"}))

if __name__ == "__main__":
    asyncio.run(run_openai_realtime_session())
