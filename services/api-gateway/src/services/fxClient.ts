const FX_URL = process.env.FX_SERVICE_URL ?? "http://localhost:3001";

export interface FxRate {
  from: string;
  to: string;
  rate: string;
  source: string;
  fetched_at: string;
}

export class FxError extends Error {
  constructor(
    public statusCode: number,
    message: string,
  ) {
    super(message);
  }
}

export async function getRate(from: string, to: string): Promise<FxRate> {
  let res: Response;
  try {
    res = await fetch(`${FX_URL}/rates?${new URLSearchParams({ from, to })}`, {
      signal: AbortSignal.timeout(3000),
    });
  } catch {
    throw new FxError(503, "fx service unavailable");
  }
  if (res.status === 404) throw new FxError(404, "rate not found");
  if (!res.ok) throw new FxError(502, "fx service error");
  return (await res.json()) as FxRate;
}