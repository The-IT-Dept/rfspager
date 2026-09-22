import { Link, useLocation, useNavigate } from "react-router";
import { BrandLockup } from "~/components/brand";
import { useAuth } from "~/lib/auth";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "~/components/ui/sidebar";
import { Separator } from "~/components/ui/separator";
import {
  LayoutDashboard,
  MessageSquare,
  Tag,
  Building2,
  Server,
  Lightbulb,
  Users as UsersIcon,
  Smartphone,
  Bell,
  MapPin,
  CreditCard,
  UserCheck,
  Flame,
  AlertTriangle,
  ShieldCheck,
  Wallet,
  LogOut,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

interface NavItem {
  label: string;
  path: string;
  icon: LucideIcon;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

const navGroups: NavGroup[] = [
  {
    label: "Overview",
    items: [
      { label: "Dashboard", path: "/admin", icon: LayoutDashboard },
    ],
  },
  {
    label: "Pager",
    items: [
      { label: "Messages", path: "/admin/messages", icon: MessageSquare },
      { label: "Capcodes", path: "/admin/capcodes", icon: Tag },
      { label: "Agencies", path: "/admin/agencies", icon: Building2 },
      { label: "Systems", path: "/admin/systems", icon: Server },
      { label: "Suggestions", path: "/admin/capcode-suggestions", icon: Lightbulb },
      { label: "Responders", path: "/admin/responders", icon: UserCheck },
    ],
  },
  {
    label: "Accounts",
    items: [
      { label: "Users", path: "/admin/users", icon: UsersIcon },
      { label: "Billing", path: "/admin/billing", icon: Wallet },
      { label: "Capcode Subs", path: "/admin/subscriptions", icon: CreditCard },
      { label: "Installations", path: "/admin/installations", icon: Smartphone },
      { label: "Alertzones", path: "/admin/alertzones", icon: MapPin },
      { label: "Notifications", path: "/admin/notifications", icon: Bell },
    ],
  },
  {
    label: "Fires Near Me",
    items: [
      { label: "Major Incidents", path: "/admin/major-incidents", icon: AlertTriangle },
      { label: "Hazard Reductions", path: "/admin/hazard-reductions", icon: Flame },
    ],
  },
  {
    label: "Settings",
    items: [
      { label: "Admin Users", path: "/admin/admin-users", icon: ShieldCheck },
    ],
  },
];

export function AppLayout({ children }: { children: React.ReactNode }) {
  const { user, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();

  const handleLogout = () => {
    logout();
    navigate("/admin/login");
  };

  return (
    <SidebarProvider>
      <Sidebar variant="inset">
        <SidebarHeader>
          <Link to="/admin" className="flex items-center px-2 py-1">
            <BrandLockup
              markClassName="h-6 w-auto text-[#ff1003]"
              wordmarkClassName="h-5 w-auto"
            />
          </Link>
        </SidebarHeader>

        <SidebarContent>
          {navGroups.map((group) => (
            <SidebarGroup key={group.label}>
              <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {group.items.map((item) => {
                    const active = location.pathname === item.path ||
                      (item.path !== "/" && location.pathname.startsWith(item.path));
                    return (
                      <SidebarMenuItem key={item.path}>
                        <SidebarMenuButton isActive={active} tooltip={item.label} render={<Link to={item.path} />}>
                          <item.icon />
                          <span>{item.label}</span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </SidebarContent>

        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="sm">
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate text-xs font-medium">{user?.name}</span>
                  <span className="truncate text-xs text-muted-foreground">{user?.email}</span>
                </div>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton size="sm" onClick={handleLogout} tooltip="Logout">
                <LogOut />
                <span>Logout</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>

      <SidebarInset className="flex flex-col overflow-hidden">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-2 !h-4" />
          <span className="text-sm font-medium text-muted-foreground">
            {navGroups.flatMap(g => g.items).find(i =>
              location.pathname === i.path || (i.path !== "/" && location.pathname.startsWith(i.path))
            )?.label || "Dashboard"}
          </span>
        </header>
        <main className="flex-1 overflow-auto p-6 min-h-0">
          {children}
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
