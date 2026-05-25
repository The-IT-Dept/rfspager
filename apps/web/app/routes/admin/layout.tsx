import { Outlet, useNavigate } from "react-router";
import { useEffect } from "react";
import { AppLayout } from "~/components/app-layout";
import { useAuth } from "~/lib/auth";

export default function ProtectedLayout() {
  const { user, loading } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (!loading && !user) navigate("/admin/login");
  }, [loading, user, navigate]);

  if (loading) return <div className="flex items-center justify-center min-h-screen text-muted-foreground">Loading...</div>;
  if (!user) return null;

  return (
    <AppLayout>
      <Outlet />
    </AppLayout>
  );
}
