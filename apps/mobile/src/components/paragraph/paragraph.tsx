import * as React from "react"
import { StyleSheet } from "react-native"
import { Text } from "react-native"

export interface ParagraphProps {
  children: React.ReactNode
}

export const Paragraph = function Paragraph(props: ParagraphProps) {
  return <Text style={styles.text} {...props} />
}

const styles = StyleSheet.create({
  text: {
    fontSize: 15,
    lineHeight: 21,
    marginBottom: 12,
    textAlign: "center",
  },
})
