//
//  VideoItemView.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 24.04.2025.
//

import SwiftUI

struct VideoItemView: View {
    let video: VideoItemModel
    let onSelect: (VideoItemModel) -> Void

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 6) {
                Text(video.name)
                    .font(.headline)
                    .foregroundStyle(Theme.textPrimary)
                    .lineLimit(1)

                HStack(alignment: .center, spacing: 12) {
                    if let date = video.createdDate {
                        Label(date.formatted(date: .numeric, time: .shortened), systemImage: "calendar")
                            .font(.caption2)
                            .foregroundStyle(Theme.textSecondary)
                    }

                    Label(video.formatDuration(), systemImage: "clock")
                        .font(.caption2)
                        .foregroundStyle(Theme.textSecondary)

                    Label(video.formatSize(), systemImage: "externaldrive")
                        .font(.caption2)
                        .foregroundStyle(Theme.textSecondary)
                }
            }
            .frame(minWidth: 100, maxWidth: .infinity, alignment: .leading)

            Button(action: {
                onSelect(video)
            }) {
                Image(systemName: "play.fill")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 20, height: 20)
                    .padding(16)
                    .background(Theme.background)
                    .foregroundColor(Theme.playback)
                    .clipShape(Circle())
            }
            .buttonStyle(BorderlessButtonStyle())
        }
        .padding(.vertical, 8)
        .padding(.horizontal, 12)
        .background(Theme.surface)
        .cornerRadius(12)
    }
}
