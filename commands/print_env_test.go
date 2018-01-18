package commands_test

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/cloudfoundry/bosh-bootloader/commands"
	"github.com/cloudfoundry/bosh-bootloader/fakes"
	"github.com/cloudfoundry/bosh-bootloader/storage"
	"github.com/cloudfoundry/bosh-bootloader/terraform"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PrintEnv", func() {
	var (
		logger           *fakes.Logger
		stderrLogger     *fakes.Logger
		stateValidator   *fakes.StateValidator
		terraformManager *fakes.TerraformManager
		sshKeyGetter     *fakes.SSHKeyGetter
		credhubGetter    *fakes.CredhubGetter
		fileIO           *fakes.FileIO
		printEnv         commands.PrintEnv
		state            storage.State
	)

	BeforeEach(func() {
		logger = &fakes.Logger{}
		stderrLogger = &fakes.Logger{}
		stateValidator = &fakes.StateValidator{}
		terraformManager = &fakes.TerraformManager{}
		sshKeyGetter = &fakes.SSHKeyGetter{}
		sshKeyGetter.GetCall.Returns.PrivateKey = "some-private-key"
		credhubGetter = &fakes.CredhubGetter{}
		credhubGetter.GetServerCall.Returns.Server = "some-credhub-server"
		credhubGetter.GetCertsCall.Returns.Certs = "some-credhub-certs"
		credhubGetter.GetPasswordCall.Returns.Password = "some-credhub-password"

		fileIO = &fakes.FileIO{}

		state = storage.State{
			BOSH: storage.BOSH{
				DirectorUsername: "some-director-username",
				DirectorPassword: "some-director-password",
				DirectorAddress:  "some-director-address",
				DirectorSSLCA:    "some-director-ca-cert",
			},
			Jumpbox: storage.Jumpbox{
				URL: "some-magical-jumpbox-url:22",
			},
		}

		printEnv = commands.NewPrintEnv(logger, stderrLogger, stateValidator, sshKeyGetter, credhubGetter, terraformManager, fileIO)
	})
	Describe("CheckFastFails", func() {
		Context("when the state does not exist", func() {
			BeforeEach(func() {
				stateValidator.ValidateCall.Returns.Error = errors.New("failed to validate state")
			})

			It("returns an error", func() {
				err := printEnv.CheckFastFails([]string{}, storage.State{})
				Expect(err).To(MatchError("failed to validate state"))
			})
		})
	})

	Describe("Execute", func() {
		BeforeEach(func() {
			fileIO.TempDirCall.Returns.Name = "some-temp-dir"
		})

		It("prints the correct environment variables for the bosh cli", func() {
			err := printEnv.Execute([]string{}, state)
			Expect(err).NotTo(HaveOccurred())

			Expect(sshKeyGetter.GetCall.Receives.Deployment).To(Equal("jumpbox"))

			Expect(logger.PrintlnCall.Messages).To(ContainElement("export BOSH_CLIENT=some-director-username"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export BOSH_CLIENT_SECRET=some-director-password"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export BOSH_CA_CERT='some-director-ca-cert'"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export BOSH_ENVIRONMENT=some-director-address"))

			Expect(logger.PrintlnCall.Messages).To(ContainElement("export CREDHUB_SERVER=some-credhub-server"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export CREDHUB_CA_CERT='some-credhub-certs'"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export CREDHUB_USER=credhub-cli"))
			Expect(logger.PrintlnCall.Messages).To(ContainElement("export CREDHUB_PASSWORD=some-credhub-password"))

			Expect(logger.PrintlnCall.Messages).To(ContainElement(MatchRegexp(`export JUMPBOX_PRIVATE_KEY=.*[/\\]bosh_jumpbox_private.key`)))
			Expect(logger.PrintlnCall.Messages).To(ContainElement(MatchRegexp(`export BOSH_ALL_PROXY=ssh\+socks5:\/\/jumpbox@some-magical-jumpbox-url:22\?private-key=\$JUMPBOX_PRIVATE_KEY`)))
		})

		It("writes private key to file in temp dir", func() {
			err := printEnv.Execute([]string{}, state)
			Expect(err).NotTo(HaveOccurred())

			for _, line := range logger.PrintlnCall.Messages {
				if strings.HasPrefix(line, "export JUMPBOX_PRIVATE_KEY=") {
					privateKeyFilename := strings.TrimPrefix(line, "export JUMPBOX_PRIVATE_KEY=")

					Expect(privateKeyFilename).To(Equal(filepath.Join("some-temp-dir", "bosh_jumpbox_private.key")))

					Expect(fileIO.WriteFileCall.Receives[0].Filename).To(Equal(privateKeyFilename))
					Expect(fileIO.WriteFileCall.Receives[0].Contents).To(Equal([]byte("some-private-key")))
				}
			}
		})

		Context("when there is no director", func() {
			BeforeEach(func() {
				terraformManager.GetOutputsCall.Returns.Outputs = terraform.Outputs{
					Map: map[string]interface{}{"external_ip": "some-external-ip"},
				}
			})

			It("prints only the BOSH_ENVIRONMENT", func() {
				err := printEnv.Execute([]string{}, storage.State{
					NoDirector: true,
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(terraformManager.GetOutputsCall.CallCount).To(Equal(1))

				Expect(logger.PrintlnCall.Messages).To(ContainElement("export BOSH_ENVIRONMENT=https://some-external-ip:25555"))
				Expect(logger.PrintlnCall.Messages).NotTo(ContainElement("export BOSH_CLIENT=some-director-username"))
				Expect(logger.PrintlnCall.Messages).NotTo(ContainElement("export BOSH_CLIENT_SECRET=some-director-password"))
				Expect(logger.PrintlnCall.Messages).NotTo(ContainElement("export BOSH_CA_CERT='some-director-ca-cert'"))
			})
		})

		Context("failure cases", func() {
			Context("when terraform manager get outputs fails", func() {
				BeforeEach(func() {
					terraformManager.GetOutputsCall.Returns.Error = errors.New("failed to get terraform output")
				})

				It("returns an error", func() {
					err := printEnv.Execute([]string{}, storage.State{
						NoDirector: true,
					})
					Expect(err).To(MatchError("failed to get terraform output"))
				})
			})

			Context("when ssh key getter fails", func() {
				BeforeEach(func() {
					sshKeyGetter.GetCall.Returns.Error = errors.New("papaya")
				})

				It("returns an error", func() {
					err := printEnv.Execute([]string{}, storage.State{})
					Expect(err).To(MatchError("papaya"))
				})
			})

			Context("when the private key can't be written", func() {
				BeforeEach(func() {
					fileIO.WriteFileCall.Returns = []fakes.WriteFileReturn{
						{
							Error: errors.New("mango"),
						},
					}
				})

				It("returns an error", func() {
					err := printEnv.Execute([]string{}, storage.State{})
					Expect(err).To(MatchError("mango"))
				})
			})

			Context("when credhub getter fails to get the password", func() {
				BeforeEach(func() {
					credhubGetter.GetPasswordCall.Returns.Error = errors.New("fig")
				})

				It("logs a warning and prints the other information", func() {
					err := printEnv.Execute([]string{}, state)
					Expect(err).NotTo(HaveOccurred())
					Expect(stderrLogger.PrintlnCall.Messages).To(ContainElement("No credhub password found."))
					Expect(logger.PrintlnCall.Messages).To(ContainElement(MatchRegexp(`export JUMPBOX_PRIVATE_KEY=`)))
				})
			})

			Context("when credhub getter fails to get the server", func() {
				BeforeEach(func() {
					credhubGetter.GetServerCall.Returns.Error = errors.New("starfruit")
				})

				It("logs a warning and prints the other information", func() {
					err := printEnv.Execute([]string{}, state)
					Expect(err).NotTo(HaveOccurred())
					Expect(stderrLogger.PrintlnCall.Messages).To(ContainElement("No credhub server found."))
					Expect(logger.PrintlnCall.Messages).To(ContainElement(MatchRegexp(`export JUMPBOX_PRIVATE_KEY=`)))
				})
			})

			Context("when credhub getter fails to get the certs", func() {
				BeforeEach(func() {
					credhubGetter.GetCertsCall.Returns.Error = errors.New("kiwi")
				})

				It("logs a warning and prints the other information", func() {
					err := printEnv.Execute([]string{}, state)
					Expect(err).NotTo(HaveOccurred())
					Expect(stderrLogger.PrintlnCall.Messages).To(ContainElement("No credhub certs found."))
					Expect(logger.PrintlnCall.Messages).To(ContainElement(MatchRegexp(`export JUMPBOX_PRIVATE_KEY=`)))
				})
			})
		})
	})
})
