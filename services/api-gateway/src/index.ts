import { buildApp } from "./app";

const port = Number(process.env.PORT ?? 3000);

buildApp()
  .listen({ port, host: "0.0.0.0" })
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });