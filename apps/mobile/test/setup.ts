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

jest.mock('react-native/Libraries/Utilities/Platform', () => ({
  OS: 'ios',
  select: jest.fn(),
}))

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

// Mock MobX State Tree
jest.mock('mobx-state-tree', () => {
  const actualMST = jest.requireActual('mobx-state-tree')
  return {
    ...actualMST,
    getEnv: jest.fn(() => ({
      gqlHttpClient: {
        setHeaders: jest.fn(),
      },
    })),
  }
})

// Mock GraphQL client
jest.mock('graphql-request', () => ({
  GraphQLClient: jest.fn().mockImplementation(() => ({
    setHeaders: jest.fn(),
    request: jest.fn(),
  })),
}))

// Setup console warnings/errors
global.console = {
  ...console,
  warn: jest.fn(),
  error: jest.fn(),
}

// Mock Animated from react-native
jest.mock('react-native/Libraries/Animated/NativeAnimatedHelper')

// Global test utilities
global.beforeEach = beforeEach
global.afterEach = afterEach
global.beforeAll = beforeAll
global.afterAll = afterAll