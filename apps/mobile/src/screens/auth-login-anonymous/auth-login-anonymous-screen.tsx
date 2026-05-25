import React, { FC, useState } from "react"
import { StyleSheet, TouchableOpacity, View, Image, ScrollView, SafeAreaView } from "react-native"
import { useAuthStore } from "../../stores"
import { ActivityIndicator, Text } from "react-native"

import { useRouter } from "expo-router"
import { getInstallationId } from "../../utils/installation"
import { Images } from "../../constants"
import { AppTheme, useAppTheme } from "../../theme"

export const AuthLoginAnonymousScreen: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    const storeLogin = useAuthStore((s) => s.login)
    const storeRegister = useAuthStore((s) => s.register)

    const [loading, setLoading] = useState(false)

    const login = async () => {
      setLoading(true)
      const email = getInstallationId() + "@anonymous.rfspager.app"
      const password = getInstallationId() + "@anonymous.rfspager.app"

      const loginResult = await storeLogin(email.toLowerCase(), password)

      if (loginResult !== "OK") {
        await storeRegister("Anonymous User", email.toLowerCase(), password, password)
        setLoading(false)
      }
    }

    return (
      <SafeAreaView style={styles.container}>
        <ScrollView showsVerticalScrollIndicator={false}>
          <View style={styles.headerSection}>
            <Image source={Images.anonymousLogin} resizeMode="contain" style={styles.image} />
            <Text style={styles.anonymousLoginText}>Anonymous Login</Text>
          </View>

          <View style={styles.contentWrapper}>
            <View style={styles.descriptionSection}>
              <Text style={styles.anonymousLoginTextDescription}>
                RFSPager works best with a real account, as it will allow you to sync settings
                between your devices.{"\n\n"}If you don't want to create an account, you may login
                anonymously.
                {"\n\n"}You will be able to convert to a real account at any time, under account
                profile.
                {"\n\n"}If you get logged out, you may log back in on the same device, however the
                account may not be recovered if you change devices.
              </Text>

              <TouchableOpacity
                style={[styles.loginButton, loading ? styles.loginButtonDisabled : {}]}
                onPress={login}
                disabled={loading}
              >
                {loading ? (
                  <ActivityIndicator size="small" color={theme.colors.white} />
                ) : (
                  <Text style={styles.loginLabel}>Login Anonymously</Text>
                )}
              </TouchableOpacity>
            </View>

            <View style={styles.bottomSection}>
              <View style={styles.bottomTextContainer}>
                <Text style={styles.createAccount}>Want to create an account? </Text>
                <TouchableOpacity onPress={() => router.push("/(auth)/register")}>
                  <Text style={styles.signup}>Sign Up</Text>
                </TouchableOpacity>
              </View>
            </View>
          </View>
        </ScrollView>
      </SafeAreaView>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    headerSection: {
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
      paddingVertical: 40,
      paddingHorizontal: 20,
    },
    image: {
      height: 80,
      width: 80,
      marginBottom: 16,
    },
    contentWrapper: {
      flex: 1,
      paddingHorizontal: 16,
      paddingTop: 20,
    },
    anonymousLoginText: {
      fontSize: 28,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
    },
    descriptionSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    anonymousLoginTextDescription: {
      color: theme.colors.textTertiary,
      fontSize: 16,
      lineHeight: 22,
      marginBottom: 20,
    },
    loginButton: {
      backgroundColor: theme.colors.primary,
      height: 50,
      justifyContent: "center",
      alignItems: "center",
      marginTop: 20,
    },
    loginButtonDisabled: {
      backgroundColor: theme.colors.borderSecondary,
    },
    loginLabel: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.white,
    },
    bottomSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
    },
    bottomTextContainer: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
    },
    createAccount: {
      color: theme.colors.textTertiary,
      fontSize: 15,
      fontWeight: "400",
    },
    signup: {
      color: theme.colors.interactive,
      fontSize: 15,
      fontWeight: "600",
    },
  })
