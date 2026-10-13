import { type RouteConfig, index, route, layout, prefix } from "@react-router/dev/routes";

export default [
  // Public site
  layout("routes/public/layout.tsx", [
    index("routes/public/home.tsx"),
    route("pager", "routes/public/pager.tsx"),
    route("map", "routes/public/map.tsx"),
    route("systems", "routes/public/systems.tsx"),
    route("capcodes", "routes/public/capcodes.tsx"),
    route("login", "routes/public/login.tsx"),
    route("forgot-password", "routes/public/forgot-password.tsx"),
    route("reset-password", "routes/public/reset-password.tsx"),
    route("register", "routes/public/register.tsx"),
    route("subscribe", "routes/public/subscribe.tsx"),
    route("account", "routes/public/account.tsx"),

    // Linked directly from both the shipped and the new mobile app, so these
    // have to keep resolving once this stack replaces the Laravel site.
    route("privacy-policy", "routes/public/privacy-policy.tsx"),
    route("terms-of-service", "routes/public/terms-of-service.tsx"),
    route("discord", "routes/public/discord.tsx"),
    // Laravel's equivalent of /account; kept so old links do not dead-end.
    route("user/profile", "routes/public/user-profile.tsx"),
  ]),

  // Admin panel under /admin
  ...prefix("admin", [
    route("login", "routes/admin/login.tsx"),
    layout("routes/admin/layout.tsx", [
      index("routes/admin/dashboard.tsx"),
      route("users", "routes/admin/users.tsx"),
      route("users/new", "routes/admin/users.new.tsx"),
      route("users/:id", "routes/admin/users.$id.tsx"),
      route("users/:id/edit", "routes/admin/users.$id.edit.tsx"),
      route("billing", "routes/admin/billing.tsx"),
      route("billing/transactions/:id", "routes/admin/billing.transactions.$id.tsx"),
      route("capcodes", "routes/admin/capcodes.tsx"),
      route("capcodes/new", "routes/admin/capcodes.new.tsx"),
      route("capcodes/:id/edit", "routes/admin/capcodes.$id.edit.tsx"),
      route("agencies", "routes/admin/agencies.tsx"),
      route("agencies/new", "routes/admin/agencies.new.tsx"),
      route("agencies/:id/edit", "routes/admin/agencies.$id.edit.tsx"),
      route("systems", "routes/admin/systems.tsx"),
      route("systems/new", "routes/admin/systems.new.tsx"),
      route("systems/:id", "routes/admin/systems.$id.tsx"),
      route("systems/:id/edit", "routes/admin/systems.$id.edit.tsx"),
      route("messages", "routes/admin/messages.tsx"),
      route("messages/:id", "routes/admin/messages.$id.tsx"),
      route("messages/:id/edit", "routes/admin/messages.$id.edit.tsx"),
      route("installations", "routes/admin/installations.tsx"),
      route("subscriptions", "routes/admin/subscriptions.tsx"),
      route("subscriptions/new", "routes/admin/subscriptions.new.tsx"),
      route("alertzones", "routes/admin/alertzones.tsx"),
      route("alertzones/new", "routes/admin/alertzones.new.tsx"),
      route("alertzones/:id/edit", "routes/admin/alertzones.$id.edit.tsx"),
      route("notifications", "routes/admin/notifications.tsx"),
      route("responders", "routes/admin/responders.tsx"),
      route("capcode-suggestions", "routes/admin/capcode-suggestions.tsx"),
      route("major-incidents", "routes/admin/major-incidents.tsx"),
      route("major-incidents/new", "routes/admin/major-incidents.new.tsx"),
      route("major-incidents/:id/edit", "routes/admin/major-incidents.$id.edit.tsx"),
      route("hazard-reductions", "routes/admin/hazard-reductions.tsx"),
      route("hazard-reductions/new", "routes/admin/hazard-reductions.new.tsx"),
      route("hazard-reductions/:id/edit", "routes/admin/hazard-reductions.$id.edit.tsx"),
      route("admin-users", "routes/admin/admin-users.tsx"),
      route("admin-users/new", "routes/admin/admin-users.new.tsx"),
      route("admin-users/:id/edit", "routes/admin/admin-users.$id.edit.tsx"),
    ]),
  ]),
] satisfies RouteConfig;
