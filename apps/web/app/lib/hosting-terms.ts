/**
 * Hosting terms shown in the /host application wizard. A copy of
 * apps/mobile/src/screens/systems/hosting-terms.ts (keep the two in step);
 * the entity is The IT Dept Pty Ltd.
 */
export const HOSTING_TERMS_VERSION = "2026-10-14";

export const HOSTING_TERMS_SECTIONS: { title: string; body: string }[] = [
  {
    title: "1. Your gear, or ours",
    body:
      "Most hosts use their own Raspberry Pi, radio dongle and antenna. If you're somewhere we really need coverage, we may be able to lend, donate or buy the hardware, so just ask when you apply. Power and internet are on you.",
  },
  {
    title: "2. Where it lives",
    body:
      "Keep it roughly where you told us. If you move it, give us a heads-up so the coverage map stays accurate. We only ever show your suburb, never your address.",
  },
  {
    title: "3. Sharing the pages",
    body:
      "What your receiver hears is a public broadcast, and you're welcome to forward it on: to your brigade, a local group, your own dashboard or another service. We just ask that it's in good faith. Don't use it to harass anyone or get in the way of crews.",
  },
  {
    title: "4. What we do with it",
    body:
      "Everything your receiver sends us goes into RFSPager for everyone: the live feed, the map and alerts.",
  },
  {
    title: "5. No promises either way",
    body:
      "This is a community effort. Receivers drop out and internet goes down; that's fine. We can't guarantee uptime to anyone, and we don't expect it from you. We might pause or remove a receiver (we'll tell you why), and you can stop hosting whenever you like.",
  },
  {
    title: "6. Free Pro while you host",
    body:
      "While your receiver is online and picking up pages, your account gets RFSPager Pro free. If it's offline for more than about a week, that pauses until it's back.",
  },
  {
    title: "7. Use common sense",
    body:
      "Listening to these broadcasts is generally fine in Australia, but how you use what you receive is up to you. Respect people's privacy and the crews doing the work.",
  },
  {
    title: "8. If things change",
    body:
      "If we update these terms we'll let you know. You can always stop hosting if you'd rather not carry on.",
  },
];
