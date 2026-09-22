import html from "../../content/privacy-policy.html?raw";
import { LegalPage } from "~/components/legal-page";

export function meta() {
  return [
    { title: "Privacy Policy · RFSPager" },
    {
      name: "description",
      content:
        "How RFSPager collects, uses and discloses your information, and the privacy rights you have.",
    },
  ];
}

export default function PrivacyPolicy() {
  return <LegalPage title="Privacy Policy" html={html} />;
}
