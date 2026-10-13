import { Platform } from "react-native"
import { toolbarIcon } from "./toolbar-icon"

const image = { uri: "plus" }

test("iOS keeps the SF Symbol name so the header draws a native glass item", () => {
  Platform.OS = "ios"
  expect(toolbarIcon("plus", image)).toBe("plus")
})

test("Android gets an image source, because its native toolbar cannot draw SF Symbols and drops the button", () => {
  Platform.OS = "android"
  expect(toolbarIcon("plus", image)).toBe(image)
})
