# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RFSPager is a React Native mobile application for iOS and Android that provides emergency service notifications for the Rural Fire Service. Built with Expo SDK 49, the app uses TypeScript, MobX State Tree for state management, and GraphQL for API communication.

## Key Commands

### Development

```bash
# Start development server
yarn dev

# Run on specific platform
yarn android
yarn ios

# Install dependencies
yarn install
```

### Building & Deployment

```bash
# Development builds
eas build --platform ios --profile development
eas build --platform android --profile development

# Production builds
eas build --platform ios --profile release
eas build --platform android --profile release

# Submit to app stores
eas submit --platform ios --profile release
eas submit --platform android --profile release
```

### Code Quality

```bash
# Run ESLint
npx eslint .

# Format with Prettier
npx prettier --write .

# Run tests
npm test
```

## Architecture Overview

### State Management

- **Zustand** stores in `src/stores/` — auth-store, preferences-store, subscriptions-store
- Persisted to AsyncStorage

### Navigation

- **Expo Router** with file-based routing in `src/app/`
- **NativeTabs** (`expo-router/unstable-native-tabs`) for iOS liquid glass tab bar
- Entry point: `expo-router/entry` (package.json main)
- Root layout: `src/app/_layout.tsx` (providers, auth guard via `<Redirect>`)
- Tabs: Pager, Incidents, Map, Settings
- Nested routes: `/message/[id]`, `/incident/[id]`, `/subscription`, `/capcodes/`, `/alertzones/`

### Data Fetching

- **TanStack Query** hooks in `src/hooks/`
- REST API client in `src/services/api-client.ts`

### Key Services

- **Firebase**: Analytics, Crashlytics, Messaging, Performance monitoring
- **Push Notifications**: Firebase Cloud Messaging + Notifee for enhanced notifications
- **Maps**: React Native Maps for incident location display
- **Subscriptions**: react-native-iap for in-app purchases
- **Authentication**: Apple, Facebook, and anonymous login

### Directory Structure

```
src/
├── app/             # Expo Router file-based routes
│   ├── (tabs)/      # Native tab bar screens
│   ├── (auth)/      # Auth flow screens
│   ├── (onboarding)/ # Onboarding flow
│   ├── message/     # Message detail [id]
│   ├── incident/    # Incident detail [id]
│   ├── capcodes/    # Capcodes list + detail
│   └── alertzones/  # Alert zones list + create
├── components/      # Reusable UI components
├── screens/         # Screen components
├── stores/          # Zustand stores
├── hooks/           # TanStack Query hooks
├── services/        # API client, purchase service
├── theme/           # Colors, typography, spacing
├── utils/           # Helper functions
├── i18n/            # Internationalization (en, ja)
└── config/          # App configuration
```

## Development Patterns

### Component Structure

- Functional components with hooks
- Screen components use `useRouter()` / `useLocalSearchParams()` from expo-router
- Components use React Native Paper for Material Design

### Navigation

- File-based routing via Expo Router (`src/app/` directory)
- Use `router.push("/path")` for navigation, `router.back()` for going back
- Route params via `useLocalSearchParams<{ id: string }>()`
- Auth guard in root layout uses `<Redirect>` based on Zustand store state

### API Calls

REST API via typed client in `src/services/api-client.ts`. TanStack Query hooks in `src/hooks/`.

### Styling

- Theme-based system with consistent spacing, colors, and typography
- React Native Paper components for Material Design
- Custom theme defined in `app/theme/`

## Important Considerations

### Platform-Specific Code

- iOS and Android native folders contain platform-specific configurations
- Firebase and push notification setup differs per platform
- Map permissions require location access configuration

### Environment Variables

- API endpoints switch based on EAS build profile
- Firebase configurations are platform-specific
- AdMob IDs configured per platform

### Testing Approach

- Jest configured with React Native preset
- Test files co-located with components
- Mock setup in `test/setup.ts`

### Performance

- Hermes JS engine enabled for better performance
- React Native Reanimated for smooth animations
- Image optimization with expo-image

### Security

- API keys and secrets managed through EAS secrets
- Firebase configuration files excluded from version control
- Authentication tokens stored securely

## Releasing

Shipping to the App Store and Google Play (release PR, CI builds, `scripts/submit-to-stores.sh`, server deploy, troubleshooting) is documented in [docs/mobile-releases.md](../../docs/mobile-releases.md). Store release notes live in `fastlane/release-notes/<version>.txt`.
