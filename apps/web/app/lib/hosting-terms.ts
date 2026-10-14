/**
 * Hosting terms shown in the /host application wizard. A copy of
 * apps/mobile/src/screens/systems/hosting-terms.ts (keep the two in step);
 * the entity is The IT Dept Pty Ltd.
 */
export const HOSTING_TERMS_VERSION = "2026-10";

export const HOSTING_TERMS_SECTIONS: { title: string; body: string }[] = [
  {
    title: "1. Hardware is on you",
    body:
      "You provide and maintain your own Raspberry Pi 4 or 5, RTL-SDR (or compatible) USB dongle, antenna, power, and reliable internet. RFSPager does not supply, fund, or warrant any of this equipment.",
  },
  {
    title: "2. Coverage",
    body:
      "You will host your receiver at the location you describe in the request, and operate it in a way that captures public emergency-services pager traffic in that area. You'll let us know if you substantially relocate the system.",
  },
  {
    title: "3. Forwarding",
    body:
      "Decoded pages from your system are forwarded only to RFSPager. You will not re-broadcast, mirror, or sell that data to any third party.",
  },
  {
    title: "4. Data handling",
    body:
      "Once a page reaches The IT Dept Pty Ltd we process and distribute it under our standard public-information policy. You retain no exclusive rights to the forwarded data.",
  },
  {
    title: "5. Best effort, no SLA",
    body:
      "This is a volunteer community service. We make no uptime guarantees to users and you make none to us. We may suspend or revoke a system at any time. You may stop hosting at any time.",
  },
  {
    title: "6. No commercial use",
    body:
      "You will not on-sell access to your system, charge for the data it produces, or fold it into a paid product.",
  },
  {
    title: "7. Legal compliance",
    body:
      "You are responsible for confirming that monitoring these transmissions complies with the law in your jurisdiction (e.g. the Radiocommunications Act in Australia). The IT Dept Pty Ltd is not liable for misuse.",
  },
  {
    title: "8. Changes",
    body:
      "We may update these terms with reasonable notice. Continuing to host your system after an update means you accept the new terms.",
  },
];
