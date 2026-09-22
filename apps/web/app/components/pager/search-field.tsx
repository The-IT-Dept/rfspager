import { Search, X } from "lucide-react";

/** Search input shared by the pager, capcodes and map rails and toolbars. */
export function SearchField({
  value,
  onChange,
  onClear,
  busy = false,
  placeholder = "Search",
  label = placeholder,
  className = "",
}: {
  value: string;
  onChange: (v: string) => void;
  onClear: () => void;
  busy?: boolean;
  placeholder?: string;
  label?: string;
  className?: string;
}) {
  return (
    <div className={`relative ${className}`}>
      <Search
        className={`pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 ${
          busy ? "animate-pulse text-[#F7241D]" : "text-gray-400"
        }`}
        aria-hidden
      />
      <input
        type="search"
        inputMode="search"
        autoComplete="off"
        enterKeyHint="search"
        aria-label={label}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-10 w-full rounded-lg border border-gray-200 bg-gray-50 pl-9 pr-9 text-sm text-gray-900 outline-none transition-colors placeholder:text-gray-400 focus:border-[#F7241D] focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          onClick={onClear}
          aria-label="Clear search"
          className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-gray-400 outline-none hover:text-gray-700 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}
