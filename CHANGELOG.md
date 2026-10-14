# Changelog

## [0.3.0](https://github.com/chenasraf/decky-emudeck-romm/compare/v0.2.0...v0.3.0) (2026-10-14)


### Features

* **saves:** convert game boy saves between emulators with saveporter ([1c8fcfe](https://github.com/chenasraf/decky-emudeck-romm/commit/1c8fcfe91dd18366f2c7b0a538eca6c5a6a9312e))
* **saves:** sync game boy and game boy color save states with RomM ([aa2c323](https://github.com/chenasraf/decky-emudeck-romm/commit/aa2c32327fff4c3ffefe2d9213472c7e92401455))


### Bug Fixes

* **saves:** keep backups taken in the same millisecond ([9da2966](https://github.com/chenasraf/decky-emudeck-romm/commit/9da296662ca9b59a896446dbfe1b4f66cc4acc37))

## [0.2.0](https://github.com/chenasraf/decky-emudeck-romm/compare/v0.1.1...v0.2.0) (2026-10-14)


### Features

* **saves:** convert GBA save states between RetroArch, mGBA and RomM's web player ([3ebedec](https://github.com/chenasraf/decky-emudeck-romm/commit/3ebedec3385828274840880d017b256d8d83bcd2))
* **saves:** sync GBA save states with RomM ([95c9cfc](https://github.com/chenasraf/decky-emudeck-romm/commit/95c9cfcfb0647ef42169e2621f502790d3944b55))

## [0.1.1](https://github.com/chenasraf/decky-emudeck-romm/compare/v0.1.0...v0.1.1) (2026-10-14)


### Bug Fixes

* **saves:** negotiate syncs in batches RomM accepts ([7927361](https://github.com/chenasraf/decky-emudeck-romm/commit/7927361bc5f96c5aaa795737cfebf53b6f77b0c7))

## 0.1.0 (2026-10-14)


### Features

* add game art to ES-DE ([2eb7ef9](https://github.com/chenasraf/decky-emudeck-romm/commit/2eb7ef9eb6981c4a0c1925f4d8f42b732fa5b496))
* adopt or link games already on the Deck ([c2b51ef](https://github.com/chenasraf/decky-emudeck-romm/commit/c2b51ef349e5741533805f498003a827a814fbe6))
* ask RomM to convert unplayable formats, and say when a game needs installing ([ad57811](https://github.com/chenasraf/decky-emudeck-romm/commit/ad578114de59e8d3e05c6c82d1b736e47fbefbb7))
* browse RomM collections and download all their missing games ([2f25e0f](https://github.com/chenasraf/decky-emudeck-romm/commit/2f25e0f7f547732a211a8c3b1c0ce4b0be5213c4))
* cache covers on the backend and show them in the library ([8afac08](https://github.com/chenasraf/decky-emudeck-romm/commit/8afac08e16f62059201eb58dacf147d8b9e16d66))
* catalog sync and QAM library browse ([ca6a92a](https://github.com/chenasraf/decky-emudeck-romm/commit/ca6a92a1ff57f5e47cf62a692c008e5beefa1a51))
* check free space before downloading and hold the queue when the disk fills ([990f88a](https://github.com/chenasraf/decky-emudeck-romm/commit/990f88a17b62ce3036dd16470a4afbf273b68f5b))
* check the connection in the background ([f09d91b](https://github.com/chenasraf/decky-emudeck-romm/commit/f09d91b56cc8be47a1276e23cf417349268bd70e))
* debug logging and a failed state for device pairing ([649712e](https://github.com/chenasraf/decky-emudeck-romm/commit/649712e5539f5429646832a9bd620c7af87c8a39))
* delete downloaded games, refusing changed files ([dcf001e](https://github.com/chenasraf/decky-emudeck-romm/commit/dcf001ed4cba900943214234bab3f7ae5779d8f5))
* **delete:** opt-in delete saves into backups when synced ([a61f349](https://github.com/chenasraf/decky-emudeck-romm/commit/a61f3493863dcfe8f770c6f06db30c4314f3701b))
* detect game updates on RomM ([482d211](https://github.com/chenasraf/decky-emudeck-romm/commit/482d2111247d89ea75ef1ef6f0810da6989b212d))
* **dev:** run the plugin in a browser against the real backend ([767646d](https://github.com/chenasraf/decky-emudeck-romm/commit/767646d902917240e86b52fd83c1c5aa7abd23c1))
* download all missing games of a platform, queueing what fits ([8af73a8](https://github.com/chenasraf/decky-emudeck-romm/commit/8af73a8c8c64718d98f50f6d23925673103b79ed))
* download BIOS per platform, with a nudge and auto-queue on first game ([3a60a34](https://github.com/chenasraf/decky-emudeck-romm/commit/3a60a3471f5e1dd34108e24fc49700edf9e2e904))
* download single-file games into EmuDeck ([de33270](https://github.com/chenasraf/decky-emudeck-romm/commit/de33270c9f73b2e0ed7e7c7010f285ff2c0c904d))
* drop or hand back owned games changed on disk ([c33347a](https://github.com/chenasraf/decky-emudeck-romm/commit/c33347a8f8a0dad34f87fb716982eb15f9d771e7))
* edit server address and forget server ([28052ac](https://github.com/chenasraf/decky-emudeck-romm/commit/28052ac5694f757a1bba3adc87502e3bb7cd0ebc))
* extract archives per system, with a Keep archives toggle ([87768f7](https://github.com/chenasraf/decky-emudeck-romm/commit/87768f78e9da0a4ad8388109950bfd0121ef5cde))
* find games kept under other names ([a3ddff7](https://github.com/chenasraf/decky-emudeck-romm/commit/a3ddff78558cf9e1db2768d3db03e48a39f1fe56))
* full-screen game detail page ([2e9ebe5](https://github.com/chenasraf/decky-emudeck-romm/commit/2e9ebe5d47a862594523221e61517df14d2607f3))
* full-screen library with shelves ([cabb059](https://github.com/chenasraf/decky-emudeck-romm/commit/cabb05928713eadc28d0a5268b1d7cd15aa3235d))
* hold extraction while a game runs ([bea616f](https://github.com/chenasraf/decky-emudeck-romm/commit/bea616f743129e6b6888ec476a85d5f245489ac2))
* ignore a game update ([f2a1076](https://github.com/chenasraf/decky-emudeck-romm/commit/f2a10764e57651d45e55bf0e7f540da3f22b837c))
* install-state probe and setup status page ([fa3973c](https://github.com/chenasraf/decky-emudeck-romm/commit/fa3973c267ba33fe683a7adbecc38704fa42d60e))
* keep games re-imported or removed on RomM ([67f5f01](https://github.com/chenasraf/decky-emudeck-romm/commit/67f5f016e3f249ff323b04fbe75ff3ef8a2848f9))
* lay out multi-file games per system ([a6defd3](https://github.com/chenasraf/decky-emudeck-romm/commit/a6defd355adf2f56590436ad70949dda118933c2))
* **library:** split platform toolbar and draw text covers for missing art ([79e6cd9](https://github.com/chenasraf/decky-emudeck-romm/commit/79e6cd900fcba43c00dbc9d0f95b5d6fbb1e56c4))
* **lifecycle:** sign out, clear cache, reset plugin data, restore backup and uninstall cleanup ([aeb368c](https://github.com/chenasraf/decky-emudeck-romm/commit/aeb368cef2046e9fd605ee0240a058261e24c10f))
* log save sync activity and restore backups ([2fce706](https://github.com/chenasraf/decky-emudeck-romm/commit/2fce7068defa02a5ccb987d7083eaba738a0bb6f))
* park and resolve save conflicts ([544020e](https://github.com/chenasraf/decky-emudeck-romm/commit/544020e3e22afd669c092e58782638998871bca8))
* password sign-in through the device flow ([a80f0b5](https://github.com/chenasraf/decky-emudeck-romm/commit/a80f0b5bb1cac9763b6d6ebe04aef6c800a84219))
* per-game save slot picker ([c6a2c27](https://github.com/chenasraf/decky-emudeck-romm/commit/c6a2c27aee1c156766c309da81def0a9ecd014ce))
* pick which EmuDeck folder a platform's games install to ([ef77fce](https://github.com/chenasraf/decky-emudeck-romm/commit/ef77fce993ca53b4abda57c8d53ef52d1b204689))
* platform grid with filters and sort ([cc36b06](https://github.com/chenasraf/decky-emudeck-romm/commit/cc36b060f55edb348514939ee07425ef2131d77e))
* plugin skeleton with supervised Go backend ([4f9047a](https://github.com/chenasraf/decky-emudeck-romm/commit/4f9047aa65fec879d9ecafdf24e0573e74178a62))
* **qam:** reorder sections and move settings to their own page ([57c5cac](https://github.com/chenasraf/decky-emudeck-romm/commit/57c5cacbec2a5874eba446452d07a3bc45a30c3a))
* QR device pairing with a stable device identity ([1af30d1](https://github.com/chenasraf/decky-emudeck-romm/commit/1af30d13b95c81497b9ebce7e176114d71b6b2f8))
* retry, resume and keep the download queue across restarts ([63a95bc](https://github.com/chenasraf/decky-emudeck-romm/commit/63a95bcf1a31108a5601027109698fec65503b29))
* **saves:** follow emulator save settings and pause sync on drift ([3ec6108](https://github.com/chenasraf/decky-emudeck-romm/commit/3ec6108a4e1a46ed85757aa1502b6bfdccfcd4af))
* **saves:** icons on sync buttons and activity entries ([6547ad2](https://github.com/chenasraf/decky-emudeck-romm/commit/6547ad29147ba9dce7bade1aaf5359b73608ff08))
* **saves:** sync RetroArch saves of games kept as archives ([03cc947](https://github.com/chenasraf/decky-emudeck-romm/commit/03cc9471162ebc02e34aba324cdf121450b68270))
* search the library from the QAM ([03f7af6](https://github.com/chenasraf/decky-emudeck-romm/commit/03f7af61caa53233c54dd9e5136a055f88a93593))
* search within a platform's game list ([ce80eb8](https://github.com/chenasraf/decky-emudeck-romm/commit/ce80eb8f7104f6c34eecdbaca1f4355ac22f239e))
* server URL and pair-code sign-in ([f15f8e7](https://github.com/chenasraf/decky-emudeck-romm/commit/f15f8e791185e602eb39f04bf757dd0e458875c3))
* signed out state and sign in again ([a34e367](https://github.com/chenasraf/decky-emudeck-romm/commit/a34e36703fa03bbebfcd2c23fb3d3d405708927f))
* stop managing a game ([d2eb9f4](https://github.com/chenasraf/decky-emudeck-romm/commit/d2eb9f45b778fd8a96d689e969a1dc3f862d13a2))
* sync DuckStation memory cards ([84661eb](https://github.com/chenasraf/decky-emudeck-romm/commit/84661eb4c4e9548acd56c3bfef1b6fce9ba26a00))
* sync melonDS and mGBA saves ([284b14c](https://github.com/chenasraf/decky-emudeck-romm/commit/284b14c7a2e0280a54e7b489ad699c062e3320c3))
* sync RetroArch saves with RomM ([3e2c2fe](https://github.com/chenasraf/decky-emudeck-romm/commit/3e2c2fe487c95ff1c38c4cfeddcb40756d534583))
* sync RMG saves ([734d78b](https://github.com/chenasraf/decky-emudeck-romm/commit/734d78be6da770464b1254134c17e20c2ce9537e))
* sync saves of linked games and unlink them ([81202c5](https://github.com/chenasraf/decky-emudeck-romm/commit/81202c58ac04e8a97eb54c1610971da5eeb545a3))
* sync saves on their own ([3865ef2](https://github.com/chenasraf/decky-emudeck-romm/commit/3865ef2a3c57807ad9ca4c624d309c1056cc7232))
* tell when ES-DE needs a rescan ([99b537e](https://github.com/chenasraf/decky-emudeck-romm/commit/99b537e942d2b549130927ab5b1c109077c5154d))
* update games in place ([ad90bed](https://github.com/chenasraf/decky-emudeck-romm/commit/ad90bed1a4589c789263ac5afafcbfead4e83f25))
* **updates:** carry saves over to the name an update gives the game ([967a68d](https://github.com/chenasraf/decky-emudeck-romm/commit/967a68d7544b26c4f63e39cc141544d62282d026))


### Bug Fixes

* **backend:** build the plugin zip with go 1.26 ([e4e50c3](https://github.com/chenasraf/decky-emudeck-romm/commit/e4e50c36142be000e44c58adea1346a3abfd48ac))
* **ci:** give the smoke test the plugin dirs the backend needs ([b7de066](https://github.com/chenasraf/decky-emudeck-romm/commit/b7de066140664faffb84e415fe3ed8f7024d80dc))
* **dev:** mock ModalRoot ([b52d6e9](https://github.com/chenasraf/decky-emudeck-romm/commit/b52d6e9a339e11964db06b9ee982301f2d12f9d0))
* **dev:** wait for the backend build and coalesce rebuilds ([6782810](https://github.com/chenasraf/decky-emudeck-romm/commit/6782810ad7949613e6b0724822195f651eb70f34))
* **find:** give identical files of a game their own paths ([a5546e4](https://github.com/chenasraf/decky-emudeck-romm/commit/a5546e4ba904230ceba726651e4bf052b1e291ec))
* **find:** match same-named games by content and add adopt all ([e9db875](https://github.com/chenasraf/decky-emudeck-romm/commit/e9db875b46c07915b7422f764f34b35d4203e59a))
* **library:** show saves of linked games on the full-screen game page ([6a54ca8](https://github.com/chenasraf/decky-emudeck-romm/commit/6a54ca8d1ce704b70d0f9d7281c5436edbb395d3))
* **make:** feed the Deck's sudo password from DECK_PWD ([303a430](https://github.com/chenasraf/decky-emudeck-romm/commit/303a430024908e1668cd965de82a658971e6f452))
* place games across drives and warn when ES-DE reads another ROM folder ([d6009e5](https://github.com/chenasraf/decky-emudeck-romm/commit/d6009e59f8b1d035464f2705dae07abea7054af6))
* **routes:** scroll inside the route so the footer stays put ([ae077af](https://github.com/chenasraf/decky-emudeck-romm/commit/ae077afef0d07a2091ec3e4196b1f16545c0c465))
* **saves:** skip watched writes that leave the save as synced ([e6ea471](https://github.com/chenasraf/decky-emudeck-romm/commit/e6ea471fb180e9e353b59bf12424c35d1c789cd3))
* **ui:** fit QAM rows to the deck and match the playground to it ([73f09f0](https://github.com/chenasraf/decky-emudeck-romm/commit/73f09f0e03d741c0af0766a07261cf08b8a7b9b4))


### Miscellaneous Chores

* release 0.1.0 ([4103ed3](https://github.com/chenasraf/decky-emudeck-romm/commit/4103ed3b06eac2890c32daf399e7e133fe6b688a))
