# IVSStageSaver

This application demonstrates how video can be saved from an IVS Stages.

IVS Stages supports the [WHEP](https://www.ietf.org/archive/id/draft-murillo-whep-02.html) protocol. This allows any
WebRTC client to easily push or pull video to a IVS Stage.

### Using
This program requires a Token (used to authenticate) and a Participant ID (the video you wish to download).
When you have those two values run the program like so.

`go run . $TOKEN $PARTICIPANT_ID`

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This library is licensed under the MIT-0 License. See the LICENSE file.

