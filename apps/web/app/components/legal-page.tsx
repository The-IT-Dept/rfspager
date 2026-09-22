const heading = "font-[family-name:var(--font-heading)]";

/**
 * Long-form legal page. The body is HTML lifted verbatim from the Laravel site
 * rather than retyped, because the exact wording is what was agreed to — the
 * markup carries no classes of its own, so `prose` supplies the typography.
 */
export function LegalPage({ title, html }: { title: string; html: string }) {
  return (
    <div className="bg-white">
      <div className="max-w-3xl mx-auto px-6 py-14 md:py-20">
        <h1
          className={`${heading} text-3xl md:text-4xl font-extrabold text-gray-900 tracking-tight mb-8`}
        >
          {title}
        </h1>
        <div
          className="prose prose-gray max-w-none prose-headings:font-[family-name:var(--font-heading)] prose-headings:tracking-tight prose-a:text-[#ff1003] hover:prose-a:underline prose-img:rounded-lg"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      </div>
    </div>
  );
}
