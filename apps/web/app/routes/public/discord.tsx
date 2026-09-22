import { redirect } from "react-router";

const DISCORD_INVITE = "https://discord.gg/NSTRQ8UtSZ";

export function loader() {
  throw redirect(DISCORD_INVITE);
}

export default function Discord() {
  // The loader always redirects; this exists so the route has a component.
  return null;
}
