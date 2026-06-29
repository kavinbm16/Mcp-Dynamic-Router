import aiohttp
from livekit import agents

async def setup_agent(ctx: agents.JobContext):
    # Configure your standard voice agent
    agent = agents.VoiceAgent(
        # ...
    )

    session_id = ctx.job.id
    sidecar_url = "http://127.0.0.1:8090/v1/stream"

    # Track partial transcriptions
    @agent.on("user_transcription")
    def on_user_transcription(transcript: agents.Transcription):
        async def send_partial():
            payload = {
                "session_id": session_id,
                "transcript": transcript.text,
                "confidence": transcript.confidence,
                "final": False
            }
            async with aiohttp.ClientSession() as session:
                async with session.post(sidecar_url, json=payload) as resp:
                    if resp.status == 200:
                        pred = await resp.json()
                        if pred.get("stable"):
                            # Optionally warm up client/cache
                            pass
        
        # Dispatch the coroutine to send transcript asynchronously
        ctx.create_task(send_partial())

    # Handle final speech commitment
    @agent.on("user_speech_committed")
    def on_user_speech_committed(msg: agents.ChatMessage):
        async def send_final():
            payload = {
                "session_id": session_id,
                "transcript": msg.content,
                "final": True
            }
            async with aiohttp.ClientSession() as session:
                async with session.post(sidecar_url, json=payload) as resp:
                    if resp.status == 200:
                        pred = await resp.json()
                        res = pred["result"]
                        if res["decision"] == "selected":
                            # Bind arguments and call target tool
                            tool = res["candidates"][0]["tool"]
                            print(f"Executing tool {tool['id']} for speech commit")

        ctx.create_task(send_final())
