export type RequestGeneration = { key: string; signal: AbortSignal };

// A transport may resolve even after cancellation; identity must also match.
export class LatestRequest {
	private current: RequestGeneration | null = null;
	private controller: AbortController | null = null;
	begin(key: string): RequestGeneration {
		this.cancel();
		this.controller = new AbortController();
		this.current = { key, signal: this.controller.signal };
		return this.current;
	}
	isCurrent(request: RequestGeneration, key: string): boolean {
		return this.current === request && request.key === key && !request.signal.aborted;
	}
	cancel() { this.controller?.abort(); this.controller = null; this.current = null; }
}
