import { redirect } from "react-router";

export function loader() {
  throw redirect("/account");
}

export default function UserProfile() {
  return null;
}
