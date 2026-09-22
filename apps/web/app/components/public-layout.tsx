import { Link, Outlet, useLocation, useNavigate } from "react-router";
import { useEffect, useState, useMemo } from "react";

const NAV_LINKS = [
  { label: "Pager", path: "/pager" },
  { label: "Map", path: "/map" },
  { label: "Capcodes", path: "/capcodes" },
  // Systems (public receiver-network page) gated off for now — see
  // routes/public/systems.tsx.
];
const SOURCE_URL = import.meta.env.VITE_SOURCE_URL || "https://github.com/the-it-dept/rfspager";

interface User {
  name: string | null;
  email: string;
  subscription_valid: boolean;
}

async function md5Hex(str: string): Promise<string> {
  const data = new TextEncoder().encode(str);
  const buf = await crypto.subtle.digest("SHA-256", data); // gravatar also accepts sha256
  return Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, "0")).join("");
}

function usePublicAuth() {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [avatarHash, setAvatarHash] = useState<string>("");
  const [trigger, setTrigger] = useState(0);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onAuth = () => setTrigger((n) => n + 1);
    window.addEventListener("rfspager:auth", onAuth);
    return () => window.removeEventListener("rfspager:auth", onAuth);
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const t = localStorage.getItem("rfspager_token");
    if (!t) { setUser(null); setToken(null); return; }
    setToken(t);

    fetch("/api/v2/mobile/me", {
      headers: { Authorization: `Bearer ${t}` },
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (data) {
          setUser(data);
          md5Hex(data.email.trim().toLowerCase()).then(setAvatarHash);
        }
      })
      .catch(() => {});
  }, [trigger]);

  const logout = () => {
    const currentToken = localStorage.getItem("rfspager_token");
    localStorage.removeItem("rfspager_token");
    window.dispatchEvent(new Event("rfspager:auth"));
    setUser(null);
    setToken(null);
    if (currentToken) {
      // Use the captured token directly rather than a shared API client so a
      // 401 cannot trigger a second logout/redirect while this one completes.
      void fetch("/api/v2/mobile/auth/logout", {
        method: "POST",
        headers: { Authorization: `Bearer ${currentToken}` },
      }).catch(() => {});
    }
  };

  return { user, token, avatarHash, logout };
}

export function PublicLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isToolPage =
    location.pathname === "/pager" ||
    location.pathname === "/map" ||
    location.pathname === "/systems" ||
    location.pathname === "/capcodes";
  const { user, avatarHash, logout } = usePublicAuth();
  const [menuOpen, setMenuOpen] = useState(false);
  const [navOpen, setNavOpen] = useState(false);

  // Close the mobile nav on navigation so it never covers the new page.
  useEffect(() => {
    setNavOpen(false);
  }, [location.pathname]);

  return (
    <div className="min-h-screen flex flex-col">
      {/* Header */}
      <header className="sticky top-0 z-50 bg-white/90 backdrop-blur-md border-b border-gray-100 shadow-sm">
        <div className="max-w-[1400px] mx-auto px-4 sm:px-6 h-16 flex items-center justify-between gap-3">
          <Link to="/" className="group shrink-0">
            <img src="/logo.svg" alt="RFSPager" className="h-8 sm:h-9 w-auto group-hover:scale-105 transition-transform" />
          </Link>

          <nav className="flex items-center gap-1 min-w-0">
            {NAV_LINKS.map((link) => {
              const isActive = location.pathname === link.path;
              return (
                <Link
                  key={link.path}
                  to={link.path}
                  className={`hidden md:block px-3 lg:px-4 py-2 text-sm font-semibold rounded-lg whitespace-nowrap transition-colors ${
                    isActive
                      ? "text-[#F7241D] bg-red-50"
                      : "text-gray-600 hover:text-gray-900 hover:bg-gray-50"
                  }`}
                >
                  {link.label}
                </Link>
              );
            })}
            <div className="hidden md:block w-px h-6 bg-gray-200 mx-2" />
            <a href={SOURCE_URL} target="_blank" rel="noreferrer" className="hidden md:block px-3 py-2 text-sm font-semibold rounded-lg whitespace-nowrap text-gray-600 hover:text-gray-900 hover:bg-gray-50 transition-colors">
              Source Code
            </a>

            {user ? (
              <div className="relative">
                <button
                  onClick={() => setMenuOpen(!menuOpen)}
                  className="flex items-center gap-2 px-3 py-1.5 rounded-lg hover:bg-gray-50 transition-colors"
                >
                  <img
                    src={`https://www.gravatar.com/avatar/${avatarHash}?s=64&d=robohash`}
                    className="w-8 h-8 rounded-full"
                    alt=""
                  />
                  <span className="text-sm font-medium text-gray-700 hidden sm:block max-w-[120px] truncate">
                    {user.name || user.email.split("@")[0]}
                  </span>
                  <svg className="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </button>

                {menuOpen && (
                  <>
                    <div className="fixed inset-0 z-40" onClick={() => setMenuOpen(false)} />
                    <div className="absolute right-0 mt-2 w-56 bg-white rounded-xl shadow-lg border border-gray-100 py-2 z-50">
                      <div className="px-4 py-2 border-b border-gray-100">
                        <p className="text-sm font-semibold text-gray-900 truncate">{user.name || "User"}</p>
                        <p className="text-xs text-gray-400 truncate">{user.email}</p>
                        {user.subscription_valid && (
                          <span className="inline-block mt-1 text-[10px] font-bold text-[#F7241D] bg-red-50 px-2 py-0.5 rounded-full uppercase">Pro</span>
                        )}
                      </div>
                      <Link
                        to="/account"
                        onClick={() => setMenuOpen(false)}
                        className="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 transition-colors"
                      >
                        My Account
                      </Link>
                      <div className="my-1 border-t border-gray-100" />
                      <button
                        onClick={() => {
                          logout();
                          setMenuOpen(false);
                          navigate("/");
                        }}
                        className="w-full text-left px-4 py-2 text-sm text-red-600 hover:bg-red-50 transition-colors"
                      >
                        Sign Out
                      </button>
                    </div>
                  </>
                )}
              </div>
            ) : (
              <Link
                to="/login"
                className="shrink-0 px-4 sm:px-5 py-2 text-sm font-bold text-white bg-[#F7241D] hover:bg-[#d91f19] rounded-lg whitespace-nowrap transition-all shadow-sm hover:shadow-md"
              >
                Sign In
              </Link>
            )}

            {/* Mobile nav toggle — the links above are hidden below md. */}
            <button
              type="button"
              onClick={() => setNavOpen((o) => !o)}
              aria-label={navOpen ? "Close menu" : "Open menu"}
              aria-expanded={navOpen}
              className="md:hidden shrink-0 ml-1 p-2 -mr-1 rounded-lg text-gray-600 hover:text-gray-900 hover:bg-gray-50 transition-colors"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d={navOpen ? "M6 18L18 6M6 6l12 12" : "M4 7h16M4 12h16M4 17h16"}
                />
              </svg>
            </button>
          </nav>
        </div>

        {/* Mobile nav panel */}
        {navOpen && (
          <div className="md:hidden border-t border-gray-100 bg-white">
            <nav className="px-4 py-3 flex flex-col gap-1">
              {NAV_LINKS.map((link) => {
                const isActive = location.pathname === link.path;
                return (
                  <Link
                    key={link.path}
                    to={link.path}
                    onClick={() => setNavOpen(false)}
                    className={`px-3 py-2.5 text-base font-semibold rounded-lg transition-colors ${
                      isActive
                        ? "text-[#F7241D] bg-red-50"
                        : "text-gray-700 hover:text-gray-900 hover:bg-gray-50"
                    }`}
                  >
                    {link.label}
                  </Link>
                );
              })}
              <a href={SOURCE_URL} target="_blank" rel="noreferrer" onClick={() => setNavOpen(false)} className="px-3 py-2.5 text-base font-semibold rounded-lg text-gray-700 hover:text-gray-900 hover:bg-gray-50 transition-colors">
                Source Code
              </a>
            </nav>
          </div>
        )}
      </header>

      <main className="flex-1 flex flex-col">
        <Outlet />
      </main>

      {!isToolPage && (
        <footer className="bg-gray-950 text-gray-400">
          <div className="max-w-[1400px] mx-auto px-6 py-16">
            <div className="grid grid-cols-1 md:grid-cols-12 gap-12">
              <div className="md:col-span-5">
                <div className="mb-5">
                  <img src="/logo.svg" alt="RFSPager" className="h-9" />
                </div>
                <p className="text-sm leading-relaxed max-w-sm">
                  Real-time access to NSW PSN Emergency Services pager messages.
                  Built for firefighters, volunteers, and communities who need to stay informed.
                </p>
                <div className="flex gap-3 mt-6">
                  <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="hover:opacity-80 transition-opacity">
                    <img src="/images/app_store.svg" alt="App Store" className="h-10" />
                  </a>
                  <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="hover:opacity-80 transition-opacity">
                    <img src="/images/play_store.svg" alt="Google Play" className="h-10" />
                  </a>
                </div>
              </div>
              <div className="md:col-span-2">
                <h3 className="text-xs font-bold text-white uppercase tracking-widest mb-4">Product</h3>
                <ul className="space-y-3 text-sm">
                  <li><Link to="/pager" className="hover:text-white transition-colors">Live Feed</Link></li>
                  <li><Link to="/map" className="hover:text-white transition-colors">Map</Link></li>
                </ul>
              </div>
              <div className="md:col-span-2">
                <h3 className="text-xs font-bold text-white uppercase tracking-widest mb-4">Resources</h3>
                <ul className="space-y-3 text-sm">
                  <li><a href="https://rfspager.app/discord" className="hover:text-white transition-colors">Discord</a></li>
                  <li><a href="mailto:hello@rfspager.app" className="hover:text-white transition-colors">Support</a></li>
                  <li><a href={SOURCE_URL} target="_blank" rel="noreferrer" className="hover:text-white transition-colors">Source code (AGPLv3)</a></li>
                </ul>
              </div>
              <div className="md:col-span-3">
                <h3 className="text-xs font-bold text-white uppercase tracking-widest mb-4">Company</h3>
                <ul className="space-y-3 text-sm">
                  <li><a href="https://theitdept.au" className="hover:text-white transition-colors">The IT Dept Pty Ltd</a></li>
                  <li><a href="mailto:hello@rfspager.app" className="hover:text-white transition-colors">hello@rfspager.app</a></li>
                </ul>
              </div>
            </div>
            <div className="border-t border-white/10 mt-12 pt-8 flex flex-col md:flex-row items-center justify-between gap-4">
              <p className="text-xs text-gray-500">&copy; {new Date().getFullYear()} The IT Dept Pty Ltd. All rights reserved.</p>
              <p className="text-xs text-gray-600">Not affiliated with NSW Rural Fire Service.</p>
            </div>
          </div>
        </footer>
      )}
    </div>
  );
}
