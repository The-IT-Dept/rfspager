import React, { FC, useState } from "react"
import {
  Image,
  Platform,
  ScrollView,
  TouchableOpacity,
  View,
  TextInput,
  SafeAreaView,
} from "react-native"
import { Text, ActivityIndicator } from "react-native"
import { useRouter } from "expo-router"
import * as AppleAuthentication from "expo-apple-authentication"
import { AccessToken, LoginManager } from "react-native-fbsdk-next"

import { emailValidator, passwordValidator } from "../../utils/validators"
import { Images } from "../../constants"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"
import { useAuthStore } from "../../stores"

export const AuthLoginScreen: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const storeLogin = useAuthStore((s) => s.login)
    const storeSocialLogin = useAuthStore((s) => s.socialLogin)
    const logoutMessage = useAuthStore((s) => s.logoutMessage)

    const [loading, setLoading] = useState(false)
    const [error, setError] = useState("")
    const [email, setEmail] = useState({ value: "", error: "" })
    const [password, setPassword] = useState({ value: "", error: "" })

    const login = async () => {
      console.log("📱 [LOGIN SCREEN] Login button pressed")
      setLoading(true)
      setError("") // Clear previous errors

      console.log("📱 [LOGIN SCREEN] Validating form...")
      const emailError = emailValidator(email.value)
      const passwordError = passwordValidator(password.value)

      if (emailError || passwordError) {
        console.log("📱 [LOGIN SCREEN] Validation failed:", { emailError, passwordError })
        setEmail({ ...email, error: emailError })
        setPassword({ ...password, error: passwordError })
        setLoading(false)
        return
      }

      console.log("📱 [LOGIN SCREEN] Form validation passed, calling store login...")
      console.log("📱 [LOGIN SCREEN] Email being sent:", email.value.toLowerCase())

      const result = await storeLogin(email.value.toLowerCase(), password.value)

      console.log("📱 [LOGIN SCREEN] Login result received:", result)

      if (result !== "OK") {
        console.log("📱 [LOGIN SCREEN] Login failed with result:", result)
        setError(typeof result === "string" ? result : "Invalid email address or password")
        setLoading(false)
      } else {
        console.log("📱 [LOGIN SCREEN] Login successful!")
      }
    }

    const fbLogin = async () => {
      setLoading(true)

      const result = await LoginManager.logInWithPermissions(["public_profile", "email"])

      if (result.isCancelled) {
        setLoading(false)
        return null
      }

      const token = await AccessToken.getCurrentAccessToken()

      storeSocialLogin("facebook", token.accessToken)
        .then(() => setLoading(false))
        .finally(() => setLoading(false))
    }

    const appleLogin = async () => {
      setLoading(true)

      try {
        const credential = await AppleAuthentication.signInAsync({
          requestedScopes: [
            AppleAuthentication.AppleAuthenticationScope.FULL_NAME,
            AppleAuthentication.AppleAuthenticationScope.EMAIL,
          ],
        })

        // Apple only returns email/fullName on the FIRST sign-in. Forward them
        // so the server can create the user even when the JWT omits the email.
        const fullName = [credential.fullName?.givenName, credential.fullName?.familyName]
          .filter(Boolean)
          .join(" ")
        storeSocialLogin("apple", credential.identityToken, {
          email: credential.email ?? undefined,
          name: fullName || undefined,
        })
          .then(() => setLoading(false))
          .finally(() => setLoading(false))
      } catch (e) {
        setLoading(false)
        return null
      }
    }

    return (
      <SafeAreaView style={styles.container}>
        <ScrollView
          showsVerticalScrollIndicator={false}
          automaticallyAdjustKeyboardInsets
          keyboardShouldPersistTaps="handled"
          contentContainerStyle={{ paddingBottom: 48 }}
        >
          <View style={styles.headerSection}>
            <Image source={Images.loginScreenlogo} resizeMode="contain" style={styles.image} />
            <Text style={styles.loginText}>Login</Text>
          </View>

          <View style={styles.contentWrapper}>
            {!!logoutMessage && (
              <View style={styles.formSection}>
                <Text style={styles.logoutMessage}>{logoutMessage}</Text>
              </View>
            )}

            <View style={styles.formSection}>
              <View style={styles.inputContainer}>
                <Text style={styles.inputLabel}>Email</Text>
                <TextInput
                  placeholder="Enter your email"
                  placeholderTextColor={theme.colors.textTertiary}
                  returnKeyType="next"
                  value={email.value}
                  onChangeText={(text) => setEmail({ value: text, error: "" })}
                  autoCapitalize="none"
                  autoComplete="email"
                  textContentType="emailAddress"
                  keyboardType="email-address"
                  style={[styles.textInput, email.error ? styles.textInputError : {}]}
                />
                {!!email.error && <Text style={styles.errorText}>{email.error}</Text>}
              </View>

              <View style={styles.inputContainer}>
                <Text style={styles.inputLabel}>Password</Text>
                <View style={styles.passwordContainer}>
                  <TextInput
                    placeholder="Enter your password"
                    placeholderTextColor={theme.colors.textTertiary}
                    returnKeyType="done"
                    value={password.value}
                    onChangeText={(text) => setPassword({ value: text, error: "" })}
                    autoCapitalize="none"
                    secureTextEntry
                    style={[styles.textInput, password.error ? styles.textInputError : {}]}
                  />
                  <TouchableOpacity
                    style={styles.forgotPassword}
                    onPress={() => router.push("/(auth)/forgot-password")}
                  >
                    <Text style={styles.forgot}>Forgot?</Text>
                  </TouchableOpacity>
                </View>
                {!!password.error && <Text style={styles.errorText}>{password.error}</Text>}
              </View>

              {!!error && <Text style={styles.errorText}>{error}</Text>}

              <TouchableOpacity
                style={[styles.loginButton, loading ? styles.loginButtonDisabled : {}]}
                onPress={login}
                disabled={loading}
              >
                {loading ? (
                  <ActivityIndicator size="small" color={theme.colors.white} />
                ) : (
                  <Text style={styles.loginLabel}>Login</Text>
                )}
              </TouchableOpacity>
            </View>

            <View style={styles.socialSection}>
              <Text style={styles.loginWithText}>or login with...</Text>
              <View style={styles.imageButtonContainer}>
                <TouchableOpacity
                  style={styles.imageButton}
                  onPress={() => router.push("/(auth)/anonymous-login")}
                >
                  <Image source={Images.anonymous} style={styles.iconImage} resizeMode="contain" />
                </TouchableOpacity>
                {Platform.OS === "ios" && (
                  <TouchableOpacity style={styles.imageButton} onPress={appleLogin}>
                    <Image source={Images.apple} style={styles.iconImage} resizeMode="contain" />
                  </TouchableOpacity>
                )}
                <TouchableOpacity style={styles.imageButton} onPress={fbLogin}>
                  <Image source={Images.facebook} style={styles.iconImage} resizeMode="contain" />
                </TouchableOpacity>
              </View>
            </View>

            <View style={styles.bottomSection}>
              <View style={styles.bottomTextContainer}>
                <Text style={styles.newToRFS}>New to RFSPager? </Text>
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
