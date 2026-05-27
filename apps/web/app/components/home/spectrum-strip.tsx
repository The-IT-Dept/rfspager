/**
 * Decorative animated spectrum band used on the marketing site.
 * Pure CSS — no SSE, no auth. Evokes the real waterfall in the app.
 */
export function SpectrumStrip({ className = "" }: { className?: string }) {
  return (
    <div className={`relative overflow-hidden rounded-lg bg-[#0a0a0a] ${className}`}>
      <div
        className="absolute inset-0"
        style={{
          background:
            "repeating-linear-gradient(90deg, rgb(0,0,30) 0%, rgb(0,0,200) 8%, rgb(0,200,200) 14%, rgb(200,220,0) 18%, rgb(255,60,60) 22%, rgb(0,200,200) 28%, rgb(0,0,200) 36%, rgb(0,0,30) 44%, rgb(0,0,200) 56%, rgb(0,200,200) 64%, rgb(255,60,60) 72%, rgb(200,220,0) 78%, rgb(0,200,200) 86%, rgb(0,0,30) 100%)",
          maskImage:
            "repeating-linear-gradient(0deg, rgba(0,0,0,0.92), rgba(0,0,0,0.92) 1px, rgba(0,0,0,0.6) 1px, rgba(0,0,0,0.6) 3px)",
          WebkitMaskImage:
            "repeating-linear-gradient(0deg, rgba(0,0,0,0.92), rgba(0,0,0,0.92) 1px, rgba(0,0,0,0.6) 1px, rgba(0,0,0,0.6) 3px)",
          animation: "rfs-spectrum-scroll 18s linear infinite",
          backgroundSize: "200% 100%",
        }}
      />
      <style>{`
        @keyframes rfs-spectrum-scroll {
          0% { background-position: 0% 0; }
          100% { background-position: 200% 0; }
        }
      `}</style>
    </div>
  );
}
