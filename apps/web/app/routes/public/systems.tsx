import { redirect } from "react-router";

// The old public receiver-network page. The coverage map replaced it; the
// route stays so old links land somewhere useful.
export async function loader() {
  throw redirect("/coverage");
}
