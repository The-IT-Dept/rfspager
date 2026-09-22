import { useEffect } from "react";
import { useIsSubscriber } from "~/lib/subscriber";

// Carried over from the Laravel site, which this stack replaces. Both were lost
// in the rebuild: the new app shipped with no analytics and no ad slot at all.
const GA_ID = import.meta.env.VITE_GA_ID ?? "G-ZLQ1CVDWTK";
const ADSENSE_CLIENT = import.meta.env.VITE_ADSENSE_CLIENT ?? "ca-pub-2990192963224757";

function injectOnce(id: string, build: () => HTMLScriptElement) {
  if (typeof document === "undefined" || document.getElementById(id)) return;
  const el = build();
  el.id = id;
  document.head.appendChild(el);
}

/**
 * Google Analytics. Not gated — Laravel loaded gtag for everyone, subscriber or
 * not, and only the ads were conditional.
 */
export function GoogleAnalytics() {
  useEffect(() => {
    if (!GA_ID) return;
    injectOnce("ga-src", () => {
      const s = document.createElement("script");
      s.async = true;
      s.src = `https://www.googletagmanager.com/gtag/js?id=${GA_ID}`;
      return s;
    });
    injectOnce("ga-init", () => {
      const s = document.createElement("script");
      s.text = `window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','${GA_ID}');`;
      return s;
    });
  }, []);
  return null;
}

/**
 * AdSense, for everyone who is not a paying subscriber — the same rule Laravel
 * applied with `@if(Auth::user()?->subscription_valid !== true)`.
 *
 * Nothing loads until the subscriber state is known, so a subscriber never sees
 * an ad appear and then vanish. "No ads" is a listed Pro benefit, so showing
 * one even briefly is a promise broken.
 */
export function AdSense() {
  const isSubscriber = useIsSubscriber();
  useEffect(() => {
    if (!ADSENSE_CLIENT || isSubscriber !== false) return;
    injectOnce("adsense-src", () => {
      const s = document.createElement("script");
      s.async = true;
      s.src = `https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=${ADSENSE_CLIENT}`;
      s.crossOrigin = "anonymous";
      return s;
    });
  }, [isSubscriber]);
  return null;
}
