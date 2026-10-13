// Jest setup file for RFSPager app
import 'react-native-gesture-handler/jestSetup'

// Silence the warning https://github.com/facebook/react-native/issues/11094#issuecomment-263240420
// @ts-ignore
global.__reanimatedWorkletInit = jest.fn()

// Mock React Native modules  
jest.mock('react-native-reanimated', () => {
  const Reanimated = require('react-native-reanimated/mock')
  Reanimated.default.call = jest.fn()
  return Reanimated
})

// React Native reads Platform through its default export; tests flip
// Platform.OS to exercise the Android branches.
jest.mock('react-native/Libraries/Utilities/Platform', () => {
  const platform = {
    OS: 'ios',
    Version: 17,
    isTesting: true,
    isTV: false,
    constants: { reactNativeVersion: { major: 0, minor: 83, patch: 2 } },
    select: (spec: Record<string, unknown>) =>
      spec[platform.OS] ?? spec.native ?? spec.default,
  }
  return { __esModule: true, default: platform, ...platform }
})

// Mock AsyncStorage
jest.mock('@react-native-async-storage/async-storage', () => ({
  getItem: jest.fn(() => Promise.resolve(null)),
  setItem: jest.fn(() => Promise.resolve()),
  removeItem: jest.fn(() => Promise.resolve()),
  clear: jest.fn(() => Promise.resolve()),
  getAllKeys: jest.fn(() => Promise.resolve([])),
  multiGet: jest.fn(() => Promise.resolve([])),
  multiSet: jest.fn(() => Promise.resolve()),
  multiRemove: jest.fn(() => Promise.resolve()),
}))

// Mock expo-router (replaces @react-navigation/native in SDK 56)
jest.mock('expo-router', () => ({
  useRouter: () => ({
    push: jest.fn(),
    replace: jest.fn(),
    back: jest.fn(),
  }),
  useLocalSearchParams: () => ({}),
  useFocusEffect: jest.fn(),
  Redirect: ({ children }: any) => children,
  Stack: Object.assign(
    ({ children }: any) => children,
    {
      Screen: () => null,
      Protected: ({ children }: any) => children,
    },
  ),
}))

// Mock Expo modules
jest.mock('expo-constants', () => ({
  manifest: {},
  platform: { ios: {} },
}))

jest.mock('expo-font', () => ({
  loadAsync: jest.fn(() => Promise.resolve()),
  isLoaded: jest.fn(() => true),
}))

// Mock Firebase
jest.mock('@react-native-firebase/app', () => ({
  default: () => ({
    auth: () => ({
      currentUser: null,
    }),
  }),
}))

jest.mock('@react-native-firebase/analytics', () => ({
  default: () => ({
    logEvent: jest.fn(),
    setUserId: jest.fn(),
    setUserProperty: jest.fn(),
  }),
}))

// Setup console warnings/errors
global.console = {
  ...console,
  warn: jest.fn(),
  error: jest.fn(),
}

