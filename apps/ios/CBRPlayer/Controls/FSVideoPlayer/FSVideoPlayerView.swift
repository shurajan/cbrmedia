//
//  FSVideoPlayerView.swift
//  FSVideoPlayer
//

import SwiftUI
import AVKit

struct FSVideoPlayerView: View {

    @ObservedObject var controller: FSPlayerController
    /// Live stream mode: no seeking, a LIVE badge instead of time and scrubber.
    var live: LiveInfo?
    var onClose: (() -> Void)?

    struct LiveInfo {
        /// Shown in the center of the top bar.
        let title: String
        /// When the viewer joined: "Watching mm:ss" counts from here.
        let joinedAt: Date
    }

    @State private var showControls = true
    @State private var isScrubbing = false
    @State private var scrubTime: Double = 0
    @State private var isAspectFill = false
    @State private var interactionCount = 0

    private let autoHideDelay: Duration = .seconds(3)

    init(
        controller: FSPlayerController,
        live: LiveInfo? = nil,
        onClose: (() -> Void)? = nil
    ) {
        self.controller = controller
        self.live = live
        self.onClose = onClose
    }

    var body: some View {
        ZStack {
            Color.black
                .ignoresSafeArea()

            FSVideoPlayerLayerView(
                player: controller.player,
                videoGravity: isAspectFill ? .resizeAspectFill : .resizeAspect
            )
            .ignoresSafeArea()

            if let error = controller.errorMessage {
                errorOverlay(error)
            } else if showControls {
                controlsOverlay
                    .transition(.opacity)
            }
        }
        .contentShape(Rectangle())
        .onTapGesture {
            withAnimation(.easeInOut(duration: 0.2)) {
                showControls.toggle()
            }
        }
        .task(id: autoHideTrigger) {
            guard showControls, controller.isPlaying, !isScrubbing else { return }
            guard (try? await Task.sleep(for: autoHideDelay)) != nil else { return }
            withAnimation(.easeInOut(duration: 0.2)) {
                showControls = false
            }
        }
        .onAppear {
            controller.play()
        }
        .onDisappear {
            controller.cleanup()
        }
    }

    // Restarts the auto-hide countdown whenever visibility, playback,
    // scrubbing state, or a button interaction changes.
    private var autoHideTrigger: String {
        "\(showControls)-\(controller.isPlaying)-\(isScrubbing)-\(interactionCount)"
    }

    private func registerInteraction() {
        interactionCount += 1
    }

    // MARK: - Controls Overlay

    private var controlsOverlay: some View {
        ZStack {
            Color.black.opacity(0.35)
                .ignoresSafeArea()

            VStack {
                topBar
                Spacer()
                if !isScrubbing {
                    centerControls
                    Spacer()
                }
                if let live {
                    liveBottomBar(live)
                } else {
                    bottomBar
                }
            }
        }
    }

    // MARK: - Error Overlay

    private func errorOverlay(_ message: String) -> some View {
        ZStack {
            Color.black.opacity(0.6)
                .ignoresSafeArea()

            VStack {
                HStack {
                    overlayButton(icon: "xmark", size: 18) {
                        onClose?()
                    }
                    Spacer()
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)

                Spacer()

                VStack(spacing: 12) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 40))
                        .foregroundColor(Theme.warning)

                    Text(message)
                        .foregroundColor(.white)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 40)
                }

                Spacer()
            }
        }
    }

    // MARK: - Top Bar

    private var topBar: some View {
        HStack {
            overlayButton(icon: "xmark", size: 25) {
                onClose?()
            }

            Spacer()

            if let live {
                Text(live.title)
                    .font(.headline)
                    .foregroundColor(.white)
                    .shadow(radius: 4)
                    .lineLimit(1)
                Spacer()
            }

            overlayButton(
                icon: isAspectFill
                    ? "arrow.down.right.and.arrow.up.left"
                    : "arrow.up.left.and.arrow.down.right",
                size: 25
            ) {
                isAspectFill.toggle()
                registerInteraction()
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 16)
    }

    // MARK: - Center Controls

    @ViewBuilder
    private var centerControls: some View {
        if live != nil {
            // Live: pause/play only; play returns to the live edge.
            overlayButton(icon: controller.isPlaying ? "pause.fill" : "play.fill", size: 48) {
                controller.isPlaying ? controller.pause() : controller.playLive()
                registerInteraction()
            }
        } else {
            seekableCenterControls
        }
    }

    private var seekableCenterControls: some View {
        HStack(spacing: 64) {
            overlayButton(icon: "gobackward.10", size: 32) {
                controller.skip(by: -10)
                registerInteraction()
            }

            overlayButton(icon: controller.isPlaying ? "pause.fill" : "play.fill", size: 48) {
                controller.togglePlayPause()
                registerInteraction()
            }

            overlayButton(icon: "goforward.10", size: 32) {
                controller.skip(by: 10)
                registerInteraction()
            }
        }
    }

    // MARK: - Bottom Bar

    private var bottomBar: some View {
        VStack(spacing: 4) {
            HStack {
                Text("\(Self.formatTime(displayedTime)) / \(Self.formatTime(controller.duration))")
                    .font(.footnote.monospacedDigit())
                    .foregroundColor(.white)
                Spacer()
            }
            .padding(.horizontal, 16)

            FSVideoScrubber(
                duration: controller.duration,
                time: displayedTime,
                onScrubStarted: {
                    scrubTime = controller.currentTime
                    isScrubbing = true
                    controller.beginScrubbing()
                },
                onScrubChanged: { time in
                    scrubTime = time
                    controller.scrub(to: time)
                },
                onScrubEnded: { time in
                    controller.endScrubbing(at: time)
                    isScrubbing = false
                }
            )
            .padding(.horizontal, 12)
        }
        .padding(.bottom, 16)
    }

    // MARK: - Live Bottom Bar

    private func liveBottomBar(_ live: LiveInfo) -> some View {
        HStack(spacing: 12) {
            HStack(spacing: 6) {
                Circle()
                    .fill(Theme.recording)
                    .frame(width: 8, height: 8)
                Text("LIVE")
                    .font(.footnote.weight(.bold))
                    .tracking(0.8)
                    .foregroundColor(.white)
            }
            .padding(.horizontal, 10)
            .frame(height: 26)
            .background(Color.black.opacity(0.5), in: Capsule())

            TimelineView(.periodic(from: live.joinedAt, by: 1)) { context in
                Text("Watching \(Self.formatTime(context.date.timeIntervalSince(live.joinedAt)))")
                    .font(.footnote.monospacedDigit())
                    .foregroundColor(.white)
                    .shadow(radius: 4)
            }

            Spacer()
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 28)
    }

    private var displayedTime: Double {
        isScrubbing ? scrubTime : controller.currentTime
    }

    // MARK: - Helpers

    private func overlayButton(icon: String, size: CGFloat, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon)
                .font(.system(size: size, weight: .semibold))
                .foregroundColor(.white)
                .shadow(radius: 4)
                .frame(width: size + 28, height: size + 28)
                .contentShape(Circle())
        }
    }

    static func formatTime(_ time: Double) -> String {
        let safeTime = time.isFinite ? max(0, time) : 0
        let totalSeconds = Int(safeTime)
        let hours = totalSeconds / 3600
        let minutes = (totalSeconds % 3600) / 60
        let seconds = totalSeconds % 60

        if hours > 0 {
            return String(format: "%d:%02d:%02d", hours, minutes, seconds)
        } else {
            return String(format: "%d:%02d", minutes, seconds)
        }
    }
}
