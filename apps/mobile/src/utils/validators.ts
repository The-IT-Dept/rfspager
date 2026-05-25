export const emailValidator = (email) => {
  const re = /\S+@\S+\.\S+/

  if (!email || email.length <= 0) return "Email cannot be empty."
  if (!re.test(email)) return "Ooops! We need a valid email address."

  return ""
}

export const passwordValidator = (password) => {
  if (!password || password.length <= 0) return "Password cannot be empty."

  return ""
}

export const verifyPasswordValidator = (verifyPassword, password) => {
  if (!verifyPassword || verifyPassword.length <= 0) return "Verify Password cannot be empty."
  if (verifyPassword !== password) return "Passwords do not match"

  return ""
}

export const nameValidator = (name) => {
  if (!name || name.length <= 0) return "Name cannot be empty."

  return ""
}
