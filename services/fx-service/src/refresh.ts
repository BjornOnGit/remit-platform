interface Logger {
  info(msg: string): void;
  error(err: unknown, msg: string): void;
}

// Runs `run` every intervalMs. A tick is skipped if the previous run is still going,
// and a failed run is logged without stopping the schedule. Returns a stop function.
export function startRefresh(run: () => Promise<number>, intervalMs: number, log: Logger) {
  let running = false;
  const timer = setInterval(async () => {
    if (running) return;
    running = true;
    try {
      log.info(`refreshed ${await run()} fx rates`);
    } catch (err) {
      log.error(err, "fx refresh failed");
    } finally {
      running = false;
    }
  }, intervalMs);
  return () => clearInterval(timer);
}