// Expo reads this in preference to app.json.
//
// app.base.json is static, so it cannot point `googleServicesFile` at anything but a
// committed path — and both Firebase config files are gitignored. EAS Build
// only uploads git-tracked files, so a cloud build fails with
// "GoogleService-Info.plist is missing".
//
// EAS file-type environment variables (GOOGLE_SERVICE_INFO_PLIST,
// GOOGLE_SERVICES_JSON) are materialised on the builder and exposed as absolute
// paths. Prefer those when present; fall back to the local files so Metro and
// local builds keep working unchanged.
const appJson = require("./app.base.json");

module.exports = () => {
  if (process.env.EAS_BUILD_PROFILE === "release" && (!process.env.EXPO_PUBLIC_API_URL || !process.env.EXPO_PUBLIC_EAS_PROJECT_ID)) {
    throw new Error("Release builds require EXPO_PUBLIC_API_URL and EXPO_PUBLIC_EAS_PROJECT_ID");
  }
  const cfg = JSON.parse(JSON.stringify(appJson.expo));
  // Provider client identifiers are supplied by each deployment, not shared by forks.
  const mapsKey = process.env.EXPO_PUBLIC_GOOGLE_MAPS_API_KEY;
  if (mapsKey) cfg.android.config = { googleMaps: { apiKey: mapsKey } };
  const facebookID = process.env.EXPO_PUBLIC_FACEBOOK_APP_ID;
  const facebookToken = process.env.EXPO_PUBLIC_FACEBOOK_CLIENT_TOKEN;
  cfg.plugins = cfg.plugins.filter((plugin) => !Array.isArray(plugin) || plugin[0] !== "react-native-fbsdk-next");
  if (facebookID && facebookToken) {
    cfg.plugins.push(["react-native-fbsdk-next", { appID: facebookID, clientToken: facebookToken, displayName: cfg.name, scheme: `fb${facebookID}` }]);
  }
  const teamID = process.env.APPLE_TEAM_ID;
  cfg.plugins = cfg.plugins.map((plugin) => {
    if (Array.isArray(plugin) && plugin[0] === "expo-build-properties") {
      delete plugin[1].ios.appleTeamId;
      if (teamID) plugin[1].ios.appleTeamId = teamID;
    }
    return plugin;
  });
  cfg.scheme = ["rfspager", ...(facebookID ? [`fb${facebookID}`] : [])];
  const easProjectID = process.env.EXPO_PUBLIC_EAS_PROJECT_ID;
  if (easProjectID) cfg.extra = { ...cfg.extra, eas: { projectId: easProjectID } };
  else if (cfg.extra) delete cfg.extra.eas;

  const iosPlist = process.env.GOOGLE_SERVICE_INFO_PLIST;
  const androidJson = process.env.GOOGLE_SERVICES_JSON;

  return {
    ...cfg,
    ios: {
      ...cfg.ios,
      ...(iosPlist ? { googleServicesFile: iosPlist } : {}),
    },
    android: {
      ...cfg.android,
      ...(androidJson ? { googleServicesFile: androidJson } : {}),
    },
  };
};
