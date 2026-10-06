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
const pkg = require("./package.json");

// The last build the previous app shipped to both stores. Every new build
// number must exceed it or the stores reject the upload.
const LAST_SHIPPED_BUILD = 530040100;

// Store build number for a release-please version: 5.3.0 -> 550300000.
// Layout is 5 + major (2 digits) + minor (2) + patch (2) + revision (3), so it
// only ever increases with the version. MOBILE_BUILD_REVISION (0-999) allows
// re-uploading the same version after a rejected or failed upload.
function storeBuildNumber(version) {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(version);
  if (!m) throw new Error(`package.json version "${version}" is not MAJOR.MINOR.PATCH`);
  const [major, minor, patch] = m.slice(1).map(Number);
  const revision = Number(process.env.MOBILE_BUILD_REVISION || 0);
  if (minor > 99 || patch > 99 || !Number.isInteger(revision) || revision < 0 || revision > 999) {
    throw new Error(`cannot encode ${version} (revision ${revision}) as a store build number`);
  }
  const build = 500000000 + major * 1e7 + minor * 1e5 + patch * 1e3 + revision;
  if (build <= LAST_SHIPPED_BUILD) {
    throw new Error(`build number ${build} would not exceed the last shipped build ${LAST_SHIPPED_BUILD}`);
  }
  return build;
}

module.exports = () => {
  const isRelease = process.env.RFSPAGER_RELEASE === "1" || process.env.EAS_BUILD_PROFILE === "release";
  if (isRelease && !process.env.EXPO_PUBLIC_API_URL) {
    throw new Error("Release builds require EXPO_PUBLIC_API_URL");
  }
  const cfg = JSON.parse(JSON.stringify(appJson.expo));
  // The marketing version follows release-please (package.json), not app.base.json.
  const build = storeBuildNumber(pkg.version);
  cfg.version = pkg.version;
  cfg.ios = { ...cfg.ios, buildNumber: String(build) };
  cfg.android = { ...cfg.android, versionCode: build };
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
