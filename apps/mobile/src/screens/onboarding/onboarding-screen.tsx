import React from "react"

import { Platform, SafeAreaView, StyleSheet } from "react-native"

import OnboardingComponent from "../../utils/OnboardingComponent"

import { FirstScene } from "./FirstScene"
import { SecondScene } from "./SecondScene"
import { ThirdScene } from "./ThirdScene"
import { FourthScene } from "./FourthScene"
import { FifthScene } from "./FifthScene"
import { SixthScene } from "./SixthScene"
import { usePreferencesStore } from "../../stores"
import { useAppTheme } from "../../theme"

export const OnboardingScreen = () => {
  const setOnboarded = usePreferencesStore((s) => s.setOnboarded)
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  let scenes = [
    {
      component: FirstScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    {
      component: SecondScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    {
      component: ThirdScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    {
      component: FourthScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    {
      component: FifthScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    {
      component: SixthScene,
      backgroundColor: theme.colors.backgroundTertiary,
    },
  ]

  if (Platform.OS === "android") {
    scenes = [
      {
        component: FirstScene,
        backgroundColor: theme.colors.backgroundTertiary,
      },
      {
        component: FourthScene,
        backgroundColor: theme.colors.backgroundTertiary,
      },
      {
        component: SixthScene,
        backgroundColor: theme.colors.backgroundTertiary,
      },
    ]
  }

  return (
    <SafeAreaView style={styles.container}>
      <OnboardingComponent
        scenes={scenes}
        enableBackgroundColorTransition={true}
        onCompleted={() => {
          setOnboarded(true)
        }}
      />
    </SafeAreaView>
  )
}

const makeStyles = (theme: any) =>
  StyleSheet.create({
    blank: { flex: 1 },
    container: {
      backgroundColor: theme.colors.backgroundTertiary,
      flex: 1,
    },
    paragraph: {
      color: theme.colors.textTertiary,
      fontSize: 18,
      margin: 24,
      textAlign: "center",
    },
    title: {
      color: theme.colors.textPrimary,
      fontSize: 30,
    },
  })
