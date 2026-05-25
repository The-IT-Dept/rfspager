import React from "react"
import {
  Animated,
  Dimensions,
  NativeScrollEvent,
  NativeSyntheticEvent,
  ScrollView,
  StatusBar,
  StyleSheet,
  View,
  ViewStyle,
} from "react-native"

const windowWidth = Dimensions.get("window").width

const Colors = {
  activeColor: "#007AFF",
  inactiveColor: "rgba(128,128,128,0.3)",
}

const Styles = StyleSheet.create({
  //
  container: {
    flex: 1,
  },

  //
  // eslint-disable-next-line react-native/sort-styles
  animatedContainer: {
    display: "flex",
    flexDirection: "row",
    flex: 1,
  },

  // Scene Container
  sceneContainer: {
    flex: 1,
    paddingBottom: 100,
  },

  // Navigation Buttons
  controllerWrapper: {
    alignItems: "center",
    bottom: 0,
    justifyContent: "flex-start",
    left: 0,
    borderWidth: 2,
    borderColor: "transparent",
    paddingBottom: 40,
    position: "absolute",
    width: windowWidth,
  },
  activePageIndicatorWrapper: {
    display: "flex",
    flexDirection: "row",
    marginBottom: 10,
  },
  activePageIndicator: {
    padding: 5,
  },
  indicator: {
    width: 10,
    height: 10,
    borderRadius: 5,
    backgroundColor: Colors.inactiveColor,
  },
  indicatorActive: {
    backgroundColor: Colors.activeColor,
  },
  btnText: {
    fontSize: 13,
    fontWeight: "600",
    color: Colors.activeColor,
  },
  btnTextHiddden: {
    color: "transparent",
  },
  btnPositive: {
    padding: 15,
    display: "flex",
    alignItems: "center",
    borderRadius: 5,
  },
  btnPositiveText: {
    color: "white",
    fontWeight: "600",
  },
})

export default class OnboardingComponent extends React.Component<Props> {
  state = {
    isLastScene: false,
  }

  static defaultProps = {
    minValueSwipeAccepted: 50,
  }

  // Scene being displayed, change everytime user navigate (swipe/click) to new scene
  _currentScene = 0
  _scrollView?: ScrollView | null

  // Animated value
  _translateXValue = new Animated.Value(0)
  _currentX = 0

  UNSAFE_componentWillMount() {
    this._translateXValue.addListener(({ value }) => {
      // Keep track of transition X value, used to difine swipe direction
      this._currentX = value
    })
  }

  // When user is swiping, animate x cordinate accordingly
  _handlePanResponderMove = Animated.event([null, { dx: this._translateXValue }], {
    useNativeDriver: false,
  })

  // Handler when user stop swiping action
  _handlePanResponderEnd = (handler: NativeSyntheticEvent<NativeScrollEvent>) => {
    let previousX = this._currentScene * windowWidth,
      dx = this._currentX - previousX

    // Define swipe direction, then animate toward the nearest view and setOffset

    const minValueSwipeAccepted: number = this.props.minValueSwipeAccepted

    if (dx > minValueSwipeAccepted) {
      // Swiped left, go to next scene
      //this._nextScene();
      this._recenterScene()
    } else if (dx < -1 * minValueSwipeAccepted) {
      // Swiped right, go to previous scene
      //this._prevScene();
      this._recenterScene()
    } else {
      // Not swipe strong enough, recenter scene
      this._recenterScene()
    }
  }

  _recenterScene = () => {
    this._animateScene(this._currentScene * windowWidth)
  }

  _prevScene = () => {
    if (this._currentScene > 0) {
      this._animateToSceneNo(this._currentScene - 1)
    } else {
      this._recenterScene()
    }
  }

  /**
   * Animate to next scene
   */
  _nextScene = () => {
    let lastScene = this.props.scenes.length - 1
    if (this._currentScene < lastScene) {
      // Animate to next scene and update currentScene index
      this._animateToSceneNo(this._currentScene + 1)
    } else if (this._currentScene == lastScene) {
      // Process to main application, or any callback after the last scene
      this.props.onCompleted && this.props.onCompleted()
    } else {
      // Recenter scene in any other case, might never happend
      this._recenterScene()
    }
  }

  /**
   * Navigate to a given scene number
   *
   * @memberof OnboardingAnimate
   */
  _animateToSceneNo = (sceneNo: number) => {
    if (this._currentScene != sceneNo) {
      let toPosition = sceneNo * windowWidth
      this._currentScene = sceneNo
      this._animateScene(toPosition)
      this._updateState()
    }
  }

  // Check current scene and update isLastScene state
  _updateState = () => {
    let { _currentScene } = this
    if (_currentScene == this.props.scenes.length - 1) {
      this.setState({ isLastScene: true })
    } else if (this.state.isLastScene) {
      this.setState({ isLastScene: false })
    }
  }

  _animateScene = (toValue: number) => {
    if (toValue < this.props.scenes.length * windowWidth && this._scrollView) {
      this._scrollView.scrollTo({ x: toValue })
    }
  }

  /**
   * Get background colors from each scene, create then return an animated background style
   */
  _getTransitionBackground = () => {
    let { scenes } = this.props,
      inputRange = [],
      outputRange = [],
      i

    for (i = 0; i < scenes.length; i++) {
      inputRange.push(i * windowWidth)
      outputRange.push(scenes[i].backgroundColor)
    }

    return {
      backgroundColor: this._translateXValue.interpolate({
        inputRange,
        outputRange,
        extrapolate: "clamp",
      }),
    }
  }

  /**
   * Render actionable page
   * (page that is not included within on boarding scenes)
   */
  _renderActionablePage = () => {
    let { actionableScene, sceneContainerStyle } = this.props

    if (actionableScene) {
      return (
        <View style={sceneContainerStyle} collapsable={false}>
          {React.createElement(actionableScene)}
        </View>
      )
    }

    return null
  }

  _renderScene = (scene: Scene, index: number) => {
    let { sceneContainerStyle } = this.props,
      animatedValue = this._translateXValue.interpolate({
        inputRange: [index * windowWidth, (index + 1) * windowWidth],
        outputRange: [0, windowWidth],
      })

    return (
      <View key={`onboarding_scene_${index}`} style={sceneContainerStyle} collapsable={false}>
        {React.createElement(scene.component, { animatedValue, nextScene: this._nextScene })}
      </View>
    )
  }

  render() {
    let { scenes, enableBackgroundColorTransition, hideStatusBar } = this.props

    let containerStyle = Styles.container

    if (enableBackgroundColorTransition) {
      containerStyle = {
        ...Styles.container,
        ...this._getTransitionBackground(),
      }
    }

    return (
      <Animated.View style={containerStyle}>
        <Animated.ScrollView
          ref={(ref: ScrollView) => {
            this._scrollView = ref
          }}
          style={{ flex: 1 }}
          horizontal={true}
          showsHorizontalScrollIndicator={false}
          scrollEventThrottle={14}
          onScroll={Animated.event(
            [
              {
                nativeEvent: {
                  contentOffset: {
                    x: this._translateXValue,
                  },
                },
              },
            ],
            {
              useNativeDriver: false,
            },
          )}
          onScrollEndDrag={this._handlePanResponderEnd}
        >
          <View
            style={[
              Styles.animatedContainer,
              {
                width: windowWidth * scenes.length,
              },
            ]}
          >
            {scenes.map(this._renderScene)}
            {this._renderActionablePage()}
          </View>
        </Animated.ScrollView>

        <StatusBar hidden={hideStatusBar} />
      </Animated.View>
    )
  }
}

type Props = {
  // Mininum acceptable value for allowing navigate to next or previous scene
  minValueSwipeAccepted?: number
  // Color when indicator is showing active state
  activeColor?: string
  // Color when indicator is showing inactive state
  inactiveColor?: string
  // Style of each scene container
  sceneContainerStyle?: ViewStyle
  // Hide statusbar
  hideStatusBar?: boolean
  // Text title of the navigate to next scene button
  navigateButtonTitle?: string
  // Text title of the link to navigate to the actionable scene
  buttonActionableTitle?: string
  // Callback when click on 'Completed' butotn in the last scene
  onCompleted?: Function

  navigateButtonCompletedTitle?: string
  enableBackgroundColorTransition: boolean

  scenes: Scene[]
  actionableScene?: any
}

type Scene = {
  component: any
  backgroundColor: string
}
