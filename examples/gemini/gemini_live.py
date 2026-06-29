import asyncio
import os
import aiohttp
from google import genai
from google.genai import types

# Configurations for Gemini and the Dynamic Router sidecar
API_KEY = os.environ.get("GEMINI_API_KEY", "your-api-key")
SIDECAR_URL = "http://127.0.0.1:8090"

# Define the single meta-tool exposed to Gemini.
# This prevents registering thousands of individual tools with Gemini,
# keeping context windows small, latency low, and accuracy high.
invoke_tool_declaration = types.FunctionDeclaration(
    name="invoke_tool",
    description="Semantic Tool Router. Invokes specialized MCP tools (weather, calculations, search) by matching the intent query.",
    parameters=types.Schema(
        type=types.Type.OBJECT,
        properties={
            "intent": types.Schema(
                type=types.Type.STRING,
                description="The user query or intent description (e.g. 'weather forecast in Bengaluru')."
            ),
            "arguments": types.Schema(
                type=types.Type.OBJECT,
                description="Parameters extracted from the conversation matching the tool requirement (e.g., {'location': 'Bengaluru'})."
            )
        },
        required=["intent", "arguments"]
    )
)

async def run_gemini_live_session():
    client = genai.Client(api_key=API_KEY)
    
    # Configure the live connection to use the meta-routing tool
    config = types.LiveConnectConfig(
        response_modalities=[types.LiveModality.AUDIO],
        tools=[types.Tool(function_declarations=[invoke_tool_declaration])]
    )
    
    # Connect to Gemini 2.0 Multimodal Live API
    async with client.aio.live.connect(model="gemini-2.0-flash-exp", config=config) as session:
        print("Connected to Gemini Multimodal Live API")

        async def receive_loop():
            async for response in session.receive():
                # 1. Listen for function call triggers from Gemini
                tool_call = response.tool_call
                if tool_call is not None:
                    for call in tool_call.function_calls:
                        if call.name == "invoke_tool":
                            # Execute the tool via our dynamic router
                            await execute_and_reply_tool(session, call)

        await receive_loop()

async def execute_and_reply_tool(session, call):
    call_id = call.id
    args = call.args
    intent = args.get("intent")
    tool_args = args.get("arguments", {})

    print(f"[Gemini Tool Call] Intercepted intent: {intent}")

    # 1. Query the router to find the best candidate tool
    route_payload = {
        "utterance": intent,
        "final": True
    }
    
    async with aiohttp.ClientSession() as http_sess:
        # Route
        async with http_sess.post(f"{SIDECAR_URL}/v1/route", json=route_payload) as resp:
            if resp.status != 200:
                print("Routing failed")
                return
            route_data = await resp.json()
            
        if route_data.get("decision") != "selected":
            print(f"Abstained: {route_data.get('reason')}")
            await send_gemini_tool_output(session, call_id, {"error": route_data.get("reason")})
            return
            
        candidate_tool = route_data["candidates"][0]["tool"]
        print(f"[Router Selected] ID: {candidate_tool['id']}")

        # 2. Execute the tool via /v1/execute on the sidecar
        exec_payload = {
            "tool_id": candidate_tool["id"],
            "arguments": tool_args
        }
        
        async with http_sess.post(f"{SIDECAR_URL}/v1/execute", json=exec_payload) as resp:
            if resp.status != 200:
                err_data = await resp.json()
                await send_gemini_tool_output(session, call_id, {"error": err_data.get("error")})
                return
            exec_result = await resp.json()
            
            # Send the final tool outputs back to the Gemini session
            print(f"[Execution Success] Output: {exec_result.get('content')}")
            await send_gemini_tool_output(session, call_id, {"result": exec_result.get("content")})

async def send_gemini_tool_output(session, call_id, result):
    """Sends the function response back to Gemini to continue audio generation."""
    tool_response = types.LiveClientContent(
        turns=[
            types.Content(
                role="user",
                parts=[
                    types.Part.from_function_response(
                        name="invoke_tool",
                        id=call_id,
                        response=result
                    )
                ]
            )
        ]
    )
    await session.send(input=tool_response)

if __name__ == "__main__":
    asyncio.run(run_gemini_live_session())
