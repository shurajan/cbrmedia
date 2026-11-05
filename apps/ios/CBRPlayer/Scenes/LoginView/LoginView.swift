//
//  LoginView.swift
//  FSPlayer
//
//  Created by Alexander Bralnin on 21.04.2025.
//
import SwiftUI

struct LoginView: View {
    @EnvironmentObject private var session: SessionStorage
    @Binding             var navigationPath: [NavigationDestination]
    @StateObject private var viewModel = LoginViewModel()

    var body: some View {
        VStack {
            Spacer()

            VStack(spacing: 20) {
                HostAndPortInputView(host: $viewModel.host)

                SecureField("Password", text: $viewModel.password)
                    .themedField()

                if let error = viewModel.errorMessage {
                    Text(error)
                        .foregroundStyle(Theme.warning)
                        .multilineTextAlignment(.center)
                }

                Button {
                    Task { await viewModel.login(session: session) }
                } label: {
                    if viewModel.isLoading {
                        ProgressView()
                            .progressViewStyle(CircularProgressViewStyle(tint: Theme.onPrimary))
                    } else {
                        Text("Login")
                            .foregroundColor(Theme.onPrimary)
                    }
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.primary)
                .disabled(viewModel.isLoading)
            }
            .padding()
            .frame(maxWidth: 400)
            .padding()

            Spacer()
        }
        .themedScreenBackground()
        .navigationTitle("Login")
        .navigationBarTitleDisplayMode(.large)
        .onChange(of: viewModel.didLogin) { oldValue, newValue in
            if newValue {
                navigationPath.append(.videoList)
            }
        }
    }
}
