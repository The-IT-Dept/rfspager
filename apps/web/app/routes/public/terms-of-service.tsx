import html from "../../content/terms-of-service.html?raw";
import { LegalPage } from "~/components/legal-page";

export function meta() {
  return [
    { title: "Terms of Service · RFSPager" },
    { name: "description", content: "The terms you agree to when using RFSPager." },
  ];
}

export default function TermsOfService() {
  return <LegalPage title="Terms of Service" html={html} />;
}
