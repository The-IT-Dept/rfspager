/**
 * register screen
 * @format
 */

import React, { FC, useState } from "react"
import {
  Image,
  Linking,
  TouchableOpacity,
  View,
  Text,
  Platform,
  ScrollView,
  TextInput,
  SafeAreaView,
} from "react-native"
import { ActivityIndicator } from "react-native"
import { useRouter } from "expo-router"
import * as AppleAuthentication from "expo-apple-authentication"
import { AccessToken, LoginManager } from "react-native-fbsdk-next"

import { emailValidator, passwordValidator, nameValidator } from "../../utils/validators"
import { Images } from "../../constants"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"
import { useAuthStore } from "../../stores"

export const AuthRegisterScreen: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    const storeRegister = useAuthStore((s) => s.register)
    const storeSocialLogin = useAuthStore((s) => s.socialLogin)

    const [loading, setLoading] = useState(false)
    const [error, setError] = useState("")
    const [name, setName] = useState({ value: "", error: "" })
    const [email, setEmail] = useState({ value: "", error: "" })
    const [password, setPassword] = useState({ value: "", error: "" })

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
        console.log(e)
        setLoading(false)
        return null
      }
    }

    const register = async () => {
      setLoading(true)
      setError("") // Clear previous errors

      const emailError = emailValidator(email.value)
      const passwordError = passwordValidator(password.value)
      const nameError = nameValidator(name.value)

      if (emailError || passwordError || nameError) {
        setEmail({ ...email, error: emailError })
        setPassword({ ...password, error: passwordError })
        setName({ ...name, error: nameError })
        setLoading(false)
        return
      }

      const result = await storeRegister(name.value, email.value.toLowerCase(), password.value, password.value)

      if (result !== "OK") {
        setError(typeof result === "string" ? result : "Registration failed")
        setLoading(false)
      }
    }

    return (
      <SafeAreaView style={styles.container}>
        <ScrollView showsVerticalScrollIndicator={false}>
          <View style={styles.headerSection}>
            <Image source={Images.signUpLogo} resizeMode="contain" style={styles.image} />
            <Text style={styles.signUpText}>Sign Up</Text>
          </View>

          <View style={styles.contentWrapper}>
            <View style={styles.socialSection}>
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
              <Text style={styles.registerWithText}>or register with e-mail...</Text>
            </View>

            <View style={styles.formSection}>
              <View style={styles.inputContainer}>
                <Text style={styles.inputLabel}>Full Name</Text>
                <TextInput
                  placeholder="Enter your full name"
                  placeholderTextColor={theme.colors.textTertiary}
                  returnKeyType="next"
                  value={name.value}
                  onChangeText={(text) => setName({ value: text, error: "" })}
                  autoComplete="name"
                  textContentType="name"
                  autoCapitalize="words"
                  style={[styles.textInput, name.error ? styles.textInputError : {}]}
                />
                {!!name.error && <Text style={styles.errorText}>{name.error}</Text>}
              </View>

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
                <TextInput
                  placeholder="Enter your password"
                  placeholderTextColor={theme.colors.textTertiary}
                  returnKeyType="done"
                  autoCapitalize="none"
                  value={password.value}
                  onChangeText={(text: any) => setPassword({ value: text, error: "" })}
                  secureTextEntry
                  style={[styles.textInput, password.error ? styles.textInputError : {}]}
                />
                {!!password.error && <Text style={styles.errorText}>{password.error}</Text>}
              </View>

              {!!error && <Text style={styles.errorText}>{error}</Text>}

              <TouchableOpacity
                style={[styles.continueButton, loading ? styles.continueButtonDisabled : {}]}
                onPress={register}
                disabled={loading}
              >
                {loading ? (
                  <ActivityIndicator size="small" color={theme.colors.white} />
                ) : (
                  <Text style={styles.continueLabel}>Continue</Text>
                )}
              </TouchableOpacity>
            </View>

            <View style={styles.conditionsSection}>
              <View style={styles.conditionsTextContainer}>
                <View style={styles.row}>
                  <Text style={styles.textStyle}>By signing up, you're agree to our </Text>
                  <TouchableOpacity
                    onPress={() => Linking.openURL("https://rfspager.app/terms-of-service")}
                  >
                    <Text style={styles.link}>Terms and Conditions</Text>
                  </TouchableOpacity>
                </View>
                <View style={styles.row}>
                  <Text style={styles.textStyle}>and </Text>
                  <TouchableOpacity
                    onPress={() => Linking.openURL("https://rfspager.app/privacy-policy")}
                  >
                    <Text style={styles.link}>Privacy Policy</Text>
                  </TouchableOpacity>
                </View>
              </View>
            </View>

            <View style={styles.bottomSection}>
              <View style={styles.bottomTextContainer}>
                <Text style={styles.alreadyHaveAccount}>Already have an account? </Text>
                <TouchableOpacity onPress={() => router.push("/(auth)/login")}>
                  <Text style={styles.loginLink}>Log In</Text>
                </TouchableOpacity>
              </View>
            </View>
          </View>
        </ScrollView>
      </SafeAreaView>
    )
  }
