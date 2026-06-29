import aiohttp
from pipecat.processors.frameworks.livekit import LiveKitTranscriptionProcessor

class MCPRouterProcessor:
    def __init__(self, sidecar_url="http://127.0.0.1:8090", session_id="call-session"):
        self.url = f"{sidecar_url}/v1/stream"
        self.session_id = session_id

    async def handle_transcription(self, text: str, is_final: bool, confidence: float):
        payload = {
            "session_id": self.session_id,
            "transcript": text,
            "confidence": confidence,
            "final": is_final
        }
        
        async with aiohttp.ClientSession() as session:
            async with session.post(self.url, json=payload) as resp:
                if resp.status == 200:
                    data = await resp.json()
                    
                    # 1. Prefetch / Warm downstream resources if the route is stable
                    if data.get("stable") and not data.get("final"):
                        candidates = data["result"].get("candidates", [])
                        if candidates:
                            tool = candidates[0]["tool"]
                            print(f"[Prefetch] Warming cache for: {tool['id']}")
                    
                    # 2. Execute tool when speech is committed
                    if data.get("final"):
                        result = data["result"]
                        if result["decision"] == "selected":
                            tool = result["candidates"][0]["tool"]
                            print(f"[Commit] Dispatched tool: {tool['id']}")
                            return tool
