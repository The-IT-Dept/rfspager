import { AdSense } from "~/components/analytics";
import { PublicLayout } from "~/components/public-layout";

// Ads belong to the public site only; mounting them here rather than in root
// keeps the AdSense script off /admin entirely.
export default function Layout() {
  return (
    <>
      <AdSense />
      <PublicLayout />
    </>
  );
}
