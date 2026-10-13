# Changelog

## [5.5.0](https://github.com/The-IT-Dept/rfspager/compare/v5.4.0...v5.5.0) (2026-10-13)


### Features

* **receiver:** remote management: server-managed config, stats, commands, self-update and captures ([#14](https://github.com/The-IT-Dept/rfspager/issues/14)) ([219e8c5](https://github.com/The-IT-Dept/rfspager/commit/219e8c55ead95fa5d3c3a6034329391cbb8df7fc))

## [5.4.0](https://github.com/The-IT-Dept/rfspager/compare/v5.3.8...v5.4.0) (2026-10-12)


### Features

* **admin:** show app version adoption on the dashboard ([4df4033](https://github.com/The-IT-Dept/rfspager/commit/4df403327b798e10ac69448e63e2207183aa85ac))
* **auth:** send password resets through Libredesk ([1223c46](https://github.com/The-IT-Dept/rfspager/commit/1223c467a1011e7868cb7fa038b0e15bf5b06174))


### Bug Fixes

* **api:** report failed alert zone deletes instead of always succeeding ([fc7fe73](https://github.com/The-IT-Dept/rfspager/commit/fc7fe73f2369d8c2c67696a0aa80b6ed2069dfb8))
* **auth:** let old-app Apple users sign in by linking their verified email ([1132aed](https://github.com/The-IT-Dept/rfspager/commit/1132aed8beb8fcb307e4724ffc2b9e3b97c91474))
* **auth:** link old-app Facebook users by email too ([31176e5](https://github.com/The-IT-Dept/rfspager/commit/31176e5c00d14a67a0ba90304fd26153c45f8212))
* **auth:** match account emails case-insensitively ([7d0ff2b](https://github.com/The-IT-Dept/rfspager/commit/7d0ff2bfc655f4b7e2ee35f853154f00e88fc6df))
* **deps:** build with Go 1.27.2 and golang.org/x/net 0.60.0 ([7aca474](https://github.com/The-IT-Dept/rfspager/commit/7aca4749c2196328fd79a92cf94649390e906138))
* **iap:** expire subscriptions that lapsed without a store event ([1108bb2](https://github.com/The-IT-Dept/rfspager/commit/1108bb26d5eb1de59f72e4ec5602af4ac52a2ce0))
* **mobile:** stop the app crashing when an alert zone is deleted ([6f8f941](https://github.com/The-IT-Dept/rfspager/commit/6f8f9410515ac0ba7448d2ced256927da390dbb1))
* **push:** retire dead Android tokens instead of retrying them ([f4a05f5](https://github.com/The-IT-Dept/rfspager/commit/f4a05f5c5739d97f2283876c4e18d8d5f5d583a9))
* **web:** load the maplibre worker so the public map renders ([9714140](https://github.com/The-IT-Dept/rfspager/commit/9714140e8c8199d6e69e3e4f63802fdd84d3d82f))

## [5.3.8](https://github.com/The-IT-Dept/rfspager/compare/v5.3.7...v5.3.8) (2026-10-07)


### Bug Fixes

* **mobile:** drop the unused react-native-keychain so Play accepts the bundle ([6a6560a](https://github.com/The-IT-Dept/rfspager/commit/6a6560a673f7036fbbdb75ea5f0bbd8413db365f))

## [5.3.7](https://github.com/The-IT-Dept/rfspager/compare/v5.3.6...v5.3.7) (2026-10-07)


### Bug Fixes

* **mobile:** ship x86_64 native libraries again on Android ([be89158](https://github.com/The-IT-Dept/rfspager/commit/be89158709d7823a84882ba2aeb01052050aae02))

## [5.3.6](https://github.com/The-IT-Dept/rfspager/compare/v5.3.5...v5.3.6) (2026-10-07)


### Bug Fixes

* **mobile:** show push notifications on iOS while the app is open ([815c242](https://github.com/The-IT-Dept/rfspager/commit/815c242a8406c3b2356c74dcee39a6b8324cc99e))

## [5.3.5](https://github.com/The-IT-Dept/rfspager/compare/v5.3.4...v5.3.5) (2026-10-07)


### Bug Fixes

* **mobile:** give the Android tab bar icons, labels and app colours ([a49a007](https://github.com/The-IT-Dept/rfspager/commit/a49a007de47c8ec8d39ef4639c61990787a62669))

## [5.3.4](https://github.com/The-IT-Dept/rfspager/compare/v5.3.3...v5.3.4) (2026-10-07)


### Bug Fixes

* **mobile:** account forms no longer slide under the header ([6c8669e](https://github.com/The-IT-Dept/rfspager/commit/6c8669e017884443ec336e19394eac03d8a6941a))

## [5.3.3](https://github.com/The-IT-Dept/rfspager/compare/v5.3.2...v5.3.3) (2026-10-06)


### Bug Fixes

* **auth:** let social sign-in accounts set a password; keep the session ([57b39d2](https://github.com/The-IT-Dept/rfspager/commit/57b39d2e3327c3681da1030fb7ba7b40f4f8c166))
* **mobile:** account forms sit below the header and work without a password ([1a37338](https://github.com/The-IT-Dept/rfspager/commit/1a373388d5345ccebe1fd1586017288e9750bd8a))

## [5.3.2](https://github.com/The-IT-Dept/rfspager/compare/v5.3.1...v5.3.2) (2026-10-06)


### Bug Fixes

* **mobile:** no empty ad capsule above the tab bar for subscribers ([969c52d](https://github.com/The-IT-Dept/rfspager/commit/969c52de0e38ab2390ffcce7cd92a5318b9147c1))

## [5.3.1](https://github.com/The-IT-Dept/rfspager/compare/v5.3.0...v5.3.1) (2026-10-06)


### Bug Fixes

* **deps:** clear high and critical audit findings in web and mobile ([2c35e4f](https://github.com/The-IT-Dept/rfspager/commit/2c35e4fb6f585418704394f52bd4fa7542802c72))
* **iap:** read real store state for Apple renewals and Google cancellations ([a4048b8](https://github.com/The-IT-Dept/rfspager/commit/a4048b818e2cc825c85fa77d4ab8dfddeea9e8e2))
* **install:** replace the old Docker agent and keep radio tuning on upgrade ([c3a32e7](https://github.com/The-IT-Dept/rfspager/commit/c3a32e762c0b15f6e586311a64a91936ce1cca25))
* **web:** keep AdSense off the admin panel ([f324d45](https://github.com/The-IT-Dept/rfspager/commit/f324d452588fcd6b69be0df6bde2f4be3a2c950b))
* **web:** ship apple-app-site-association for iOS universal links ([3c60b2f](https://github.com/The-IT-Dept/rfspager/commit/3c60b2f645de58404fc2497f6feb8fe6176691d6))

## [5.3.0](https://github.com/The-IT-Dept/rfspager/compare/v5.2.1...v5.3.0) (2026-10-06)


### Features

* **iap:** handle Adapty webhooks in Go and record store IDs ([846f97a](https://github.com/The-IT-Dept/rfspager/commit/846f97ac5d5410598562a92a3f321b8665f81db7))
* **web:** restore Google Analytics and AdSense ([c81068b](https://github.com/The-IT-Dept/rfspager/commit/c81068bdc8806a1328cf298cdac8ad6e3c4c48ff))


### Bug Fixes

* **iap:** let one store entitlement back several accounts ([02e2701](https://github.com/The-IT-Dept/rfspager/commit/02e270160354c05b0744153b56e84135d981a760))
* **ingress:** make message dedupe atomic ([5afb5f9](https://github.com/The-IT-Dept/rfspager/commit/5afb5f9271ec313aade1244401e5f3057f1c614e))
* **install:** show the receiver install command without needless quotes ([18efa67](https://github.com/The-IT-Dept/rfspager/commit/18efa679dc4db5bda548e6a5d03da1eb921c65a5))
* **mobile:** correct IAP restore result and expo-iap v4 cancel code ([dad509b](https://github.com/The-IT-Dept/rfspager/commit/dad509b0a6f84ec7b4b052f236a5b2dd3e8d4be8))
* **push,iap:** unblock push delivery on prod data; never shorten gifted access ([fb7599f](https://github.com/The-IT-Dept/rfspager/commit/fb7599fee9ff4a83168cca681882f097c863b839))
* **recvhub:** stop false receiver-offline alerts after brief disconnects ([8d27949](https://github.com/The-IT-Dept/rfspager/commit/8d27949e500224c6fe252abf45f1a61555187cd2))
* **security:** remediate findings from strix deep scan ([2412a19](https://github.com/The-IT-Dept/rfspager/commit/2412a191fe307e7b67d13e371520acad37f43f23))

## [5.2.1](https://github.com/The-IT-Dept/rfspager/compare/v5.2.0...v5.2.1) (2026-09-22)


### Bug Fixes

* **receiver:** endpoints file wins over the legacy environment variables ([d1787d5](https://github.com/The-IT-Dept/rfspager/commit/d1787d5eb0f0ea746fbae888b72ad8eb8bea4afa))
* **receiver:** log legacy post failures instead of discarding them ([285c7a4](https://github.com/The-IT-Dept/rfspager/commit/285c7a4494bbfaaca210bcd8181604d8c51173ed))

## [5.2.0](https://github.com/The-IT-Dept/rfspager/compare/v5.1.0...v5.2.0) (2026-09-22)


### Features

* **receiver:** fan out to several backends, and restore legacy backfeed ([0bca195](https://github.com/The-IT-Dept/rfspager/commit/0bca195bfba35fc2b31d9a6e78da0fc6366a10a1))


### Bug Fixes

* **ci:** attach release assets to the tag, not the dispatch ref ([60cd8f0](https://github.com/The-IT-Dept/rfspager/commit/60cd8f0d1afb0175fc167b3cd02af5f22ae60a1a))
* **ci:** upload release assets with gh instead of the release action ([76ae6f7](https://github.com/The-IT-Dept/rfspager/commit/76ae6f7a1d19d00a33a499c49ecda57f4eceb734))

## [5.1.0](https://github.com/The-IT-Dept/rfspager/compare/v5.0.0...v5.1.0) (2026-09-22)


### Features

* **adapty-backfill:** add --only-transacted scoping flag ([d7d0403](https://github.com/The-IT-Dept/rfspager/commit/d7d0403a7fbd7349d0fe340d82ab7d2146ec1fd1))
* **admin:** billing dashboard, user detail page, IAP transaction log ([6ea3f27](https://github.com/The-IT-Dept/rfspager/commit/6ea3f271ca2d1e6249b0f7b69e5219db75ba1a8e))
* live marketing hero, stations map layer, map + fanout fixes ([95912f6](https://github.com/The-IT-Dept/rfspager/commit/95912f6738c55c23a754ff3b8f1389da0426c439))
* **mobile:** collapse ad debug panel behind a toggle button ([ca4c8c1](https://github.com/The-IT-Dept/rfspager/commit/ca4c8c18151b3253203dc658ac3b56df15953a7f))
* **mobile:** editable profile, change password, change email, delete account ([11b43b4](https://github.com/The-IT-Dept/rfspager/commit/11b43b462d4ab49ed2ccad02054cf7dd5dfe02c0))
* **mobile:** native iOS 26 search button and action menu in the Pager header ([bb849f9](https://github.com/The-IT-Dept/rfspager/commit/bb849f9eb451a788bb3334919e37d14fbf07d9f9))
* **mobile:** native tab bar in place of the JS drawer ([c3852b3](https://github.com/The-IT-Dept/rfspager/commit/c3852b3808395dc0a8702f4820f1badf4a5cbb0a))
* **mobile:** new "Signal" app icon and a proper splash screen ([9c5ef15](https://github.com/The-IT-Dept/rfspager/commit/9c5ef152586baa6ea338d20647ef5516beb0e976))
* **mobile:** SF Symbols and real glass in place of web icons and flat fills ([dfdd130](https://github.com/The-IT-Dept/rfspager/commit/dfdd1307935989f6e30a0e5c1786add7883598b6))
* public capcodes browser, suggestions, master/detail pager + systems ([260db26](https://github.com/The-IT-Dept/rfspager/commit/260db26b99552f1041c7655f3ccf53cc3777a1f6))
* receiver observability — event timeline, log streaming, offline watchdog ([2f0490e](https://github.com/The-IT-Dept/rfspager/commit/2f0490ee0087e48800be70037bf41425d55b6f6f))
* **receiver:** one-command install for system owners ([5fb6957](https://github.com/The-IT-Dept/rfspager/commit/5fb6957e65e2c312fe70f7af995312dbaf4ba1dc))
* **receiver:** show the real install command in the app and admin ([785fa74](https://github.com/The-IT-Dept/rfspager/commit/785fa743c435bd318946aa15b77cf81914c4688c))
* remove the spectrum waterfall end to end ([52aab8a](https://github.com/The-IT-Dept/rfspager/commit/52aab8ab13bdfd98d24e44a56ff04c7db80b0461))
* RTL-SDR receiver agent, live spectrum waterfall, owner-aware system detail ([c19fae7](https://github.com/The-IT-Dept/rfspager/commit/c19fae7bebff2d5afb88cd455e9cdfa6ac071b86))
* **server:** PATCH /api/v2/mobile/me + DELETE /api/v2/mobile/me ([b08a225](https://github.com/The-IT-Dept/rfspager/commit/b08a2255ef424b07495363a7d7a0c954d2a3a717))
* suggestion context, system on message, keyboard fixes, approve hardening ([6531db2](https://github.com/The-IT-Dept/rfspager/commit/6531db24db07186214706e1d71be5b6432338194))
* **web:** /account page with profile, password, email, subscription, delete ([ef47821](https://github.com/The-IT-Dept/rfspager/commit/ef4782193ba3a465d876a164daf7e66832525ef5))
* **web:** adopt the new app icon across the site ([46134fe](https://github.com/The-IT-Dept/rfspager/commit/46134feb5cd51b28ce61e6480919c4946956a892))
* **web:** carry over the paths the Laravel site still serves ([8d30ba4](https://github.com/The-IT-Dept/rfspager/commit/8d30ba4562d0fbf30f19d4cfec19b866e523c245))
* **web:** rebuild the public site around real data ([4d83fc1](https://github.com/The-IT-Dept/rfspager/commit/4d83fc1124ebec9a48219bca0d2ebff17661d01f))


### Bug Fixes

* **admin:** don't reuse request context for approval push notification ([771dcc8](https://github.com/The-IT-Dept/rfspager/commit/771dcc8936cefb5341f38294a39e2c0c5c3d4269))
* **billing:** correct MRR by 10x, repair Adapty backfill writes ([7394ab4](https://github.com/The-IT-Dept/rfspager/commit/7394ab438ab0b5b6e45f2db4af7aa1c34265f9a2))
* **billing:** reconcile duplicate IAP ownership instead of refusing to migrate ([6fee93f](https://github.com/The-IT-Dept/rfspager/commit/6fee93f4b2e41279b6e6fcc13308f3a210572a80))
* **billing:** use real store prices in MRR estimate ([8d94172](https://github.com/The-IT-Dept/rfspager/commit/8d94172cdda2bb2b3dfb29cf57185d384a294c71))
* **ci:** stop the auth-store guard writing to a macOS-only path ([aebd9d4](https://github.com/The-IT-Dept/rfspager/commit/aebd9d404abc5e6e0f46ab5e1f347a05a81ba629))
* **db:** make migrations apply against production data ([c07eda6](https://github.com/The-IT-Dept/rfspager/commit/c07eda65436a5c1c16d0967ce837627fb438a056))
* **db:** migration 012 shifted every timestamp by ten hours ([092f452](https://github.com/The-IT-Dept/rfspager/commit/092f452b2b4be959086817561543e07732608d41))
* harden authentication, ingress, billing and migration safety ([77c6ac7](https://github.com/The-IT-Dept/rfspager/commit/77c6ac7982a74afe111828607306879ed1e5f545))
* **ingress:** accept real Pagermon clients so Go can take ingress over ([2375cb0](https://github.com/The-IT-Dept/rfspager/commit/2375cb0baf50ba4b83c50df7d0233742a4ff1918))
* **mobile:** keep native chrome in step with the app theme; size accessory ad to slot ([ffcbb60](https://github.com/The-IT-Dept/rfspager/commit/ffcbb603afad515471069fd23b6f8f0c5bae5672))
* **mobile:** keep the Pager list mounted so the tab bar stays minimised ([aa6ecbd](https://github.com/The-IT-Dept/rfspager/commit/aa6ecbd2c248259df1d4af8e5f0ec2a637e2c6d3))
* **mobile:** let EAS cloud builds find the Firebase config files ([e377a14](https://github.com/The-IT-Dept/rfspager/commit/e377a14ccc2625abc3ca2e9a77b6cc150059f1da))
* **mobile:** link the moved screens from Settings ([ea6a16e](https://github.com/The-IT-Dept/rfspager/commit/ea6a16e10385890e402ed184d010a76570308d4c))
* **mobile:** real glass headers via ExperimentalStack; ad in tab-bar accessory ([f0934d8](https://github.com/The-IT-Dept/rfspager/commit/f0934d80f66a2ee615853f936bf54063910d391a))
* **mobile:** register the content scroll view so the chrome can react to it ([4d55d58](https://github.com/The-IT-Dept/rfspager/commit/4d55d58b4ce14fe99c1e52df572d28715a16ba25))
* **mobile:** repair nine navigations left pointing at a deleted route group ([7ae540c](https://github.com/The-IT-Dept/rfspager/commit/7ae540cb325b9a0a11fa0ba87c8e76e682af3844))
* **mobile:** show the accessory ad in the minimised (inline) placement too ([7cea450](https://github.com/The-IT-Dept/rfspager/commit/7cea4500793f112cedc95a20bfeead9e068b1cbc))
* **mobile:** stop overriding the native header, add keyboard avoidance ([fc9dd31](https://github.com/The-IT-Dept/rfspager/commit/fc9dd319a436d533d2184ccf8b319f65e29833a5))
* **pocsag:** detect noisy idle codewords before BCH misinterprets them ([2d67800](https://github.com/The-IT-Dept/rfspager/commit/2d67800a5544688132f4499ceab842e1df617403))
* prepare AGPL clients and release builds for public contribution ([efc93b4](https://github.com/The-IT-Dept/rfspager/commit/efc93b4d1c1c6f0a56560a1f2c5a63824a3727b3))
* **receiver:** block DSP, gap-aware resync, matched-filter slicer ([a794246](https://github.com/The-IT-Dept/rfspager/commit/a794246e93e51b983ba002df9fd8fb79aaac9e85))
* **sse:** keep one keepalive ticker per connection, not one per message ([df12464](https://github.com/The-IT-Dept/rfspager/commit/df12464e5ccd04fefea09b42a71990ac421b0cec))
* **web+db:** pager mobile layout, gate public systems page, drop seeded admin ([5c38d30](https://github.com/The-IT-Dept/rfspager/commit/5c38d30bf6c9675518c40d7742eae5ef0c956af3))
* **web:** repair pager infinite scroll, add feed cursor, rebuild public header ([57ebe5e](https://github.com/The-IT-Dept/rfspager/commit/57ebe5e006ded0aa74fc1d6956620082e6857d8f))


### Performance Improvements

* **mobile:** index the message feed and paginate it by cursor ([2d4b64c](https://github.com/The-IT-Dept/rfspager/commit/2d4b64cbf46bd6c59a87a8e54f7e68fa04eaa44f))
