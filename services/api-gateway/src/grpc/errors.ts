import { status } from "@grpc/grpc-js";

export interface GrpcError {
  code: number;
  details: string;
}

// Fastify errors have string codes; gRPC errors have numeric codes and `details`.
export function isGrpcError(err: unknown): err is GrpcError {
  const e = err as Record<string, unknown> | null;
  return (
    typeof e === "object" &&
    e !== null &&
    typeof e.code === "number" &&
    typeof e.details === "string"
  );
}

const HTTP_STATUS: Record<number, number> = {
  [status.INVALID_ARGUMENT]: 400,
  [status.NOT_FOUND]: 404,
  [status.ALREADY_EXISTS]: 409,
  [status.FAILED_PRECONDITION]: 422,
  [status.UNAVAILABLE]: 503,
};

export const httpStatusFor = (code: number): number => HTTP_STATUS[code] ?? 500;