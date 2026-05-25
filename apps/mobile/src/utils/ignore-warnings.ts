/**
 * Ignore some yellowbox warnings. Some of these are for deprecated functions
 * that we haven't gotten around to replacing yet.
 */
import { LogBox } from "react-native"

// prettier-ignore
LogBox.ignoreLogs([
        "new NativeEventEmitter",
        "ViewPropTypes will be",
        "No native splash screen registered for given view controller",
        "Require cycle",
        "Require cycle:",
        "Warning: Cannot update a component",
        "is not a valid icon name for family"
    ]
)
